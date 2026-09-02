package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

type Vault struct {
	aead cipher.AEAD
	Dir  string
}

func NewVault(key, dir string) (*Vault, error) {
	b, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(b) != 32 {
		return nil, errors.New("RELAY_MASTER_KEY must be a base64-encoded 32-byte key")
	}
	block, e := aes.NewCipher(b)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Join(dir, "snapshots"), 0700); e != nil {
		return nil, e
	}
	return &Vault{a, dir}, nil
}
func (v *Vault) Seal(b []byte, aad string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(v.aead.Seal(nonce, nonce, b, []byte(aad))), nil
}
func (v *Vault) Open(encoded, aad string) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(encoded)
	n := v.aead.NonceSize()
	if e != nil || len(b) < n {
		return nil, errors.New("invalid encrypted value")
	}
	return v.aead.Open(nil, b[:n], b[n:], []byte(aad))
}
func (v *Vault) Snapshot(b []byte) (string, error) {
	hash := Hash(b)
	enc, e := v.Seal(b, hash)
	if e != nil {
		return "", e
	}
	f, e := os.CreateTemp(filepath.Join(v.Dir, "snapshots"), ".pending-")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return "", e
	}
	if _, e = f.WriteString(enc); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	e = os.Rename(f.Name(), filepath.Join(v.Dir, "snapshots", hash))
	return hash, e
}
func (v *Vault) ReadSnapshot(hash string) ([]byte, error) {
	if len(hash) != 64 {
		return nil, errors.New("invalid snapshot hash")
	}
	if _, e := hex.DecodeString(hash); e != nil {
		return nil, e
	}
	b, e := os.ReadFile(filepath.Join(v.Dir, "snapshots", hash))
	if e != nil {
		return nil, e
	}
	raw, e := v.Open(string(b), hash)
	if e == nil && Hash(raw) != hash {
		return nil, errors.New("snapshot checksum mismatch")
	}
	return raw, e
}

var sensitive = map[string]bool{"token": true, "accesstoken": true, "apikey": true, "password": true, "passwd": true, "secret": true, "authorization": true, "cookie": true, "setcookie": true, "xembytoken": true, "xapikey": true, "username": true}

func SensitiveKey(k string) bool {
	k = strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(k))
	return sensitive[k] || strings.HasSuffix(k, "password") || strings.HasSuffix(k, "secret") || strings.HasSuffix(k, "token")
}
func SafeURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("only absolute HTTP(S) URLs are allowed")
	}
	if u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("URL userinfo, fragments and control characters are not allowed")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return errors.New("invalid URL query")
	}
	for k := range q {
		if SensitiveKey(k) || slices.Contains([]string{"auth", "key", "code", "session", "signature", "jwt", "credential"}, strings.ToLower(k)) {
			return errors.New("credentials belong in encrypted headers, not URLs")
		}
	}
	return nil
}

// Imported opaque rules are stored as data only. Explicit embedded credentials are rejected before persistence or publication.
func ValidateDocument(b []byte) error {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return walk(v)
}
func walk(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if SensitiveKey(k) && v != nil && v != "" {
				return errors.New("embedded credentials are not allowed; use the credential vault")
			}
			if e := walk(v); e != nil {
				return e
			}
		}
	case []any:
		for _, v := range x {
			if e := walk(v); e != nil {
				return e
			}
		}
	case string:
		if strings.HasPrefix(x, "http://") || strings.HasPrefix(x, "https://") {
			if e := SafeURL(x); e != nil {
				return e
			}
		}
	}
	return nil
}
func ValidateHeaders(h map[string]string) error {
	if len(h) > 16 {
		return errors.New("too many headers")
	}
	for k, v := range h {
		l := strings.ToLower(k)
		for _, c := range k {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return errors.New("invalid credential header name")
			}
		}
		if len(k) == 0 || len(v) > 8192 || strings.ContainsAny(k+v, "\r\n\x00") || l == "host" || l == "connection" || l == "content-length" || l == "transfer-encoding" || l == "accept-encoding" || l == "proxy-authorization" || strings.HasPrefix(l, "proxy-") {
			return errors.New("invalid credential header")
		}
	}
	return nil
}
