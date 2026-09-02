package security

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultBindsOwnerAndEncryptsSnapshots(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	v, e := NewVault(base64.StdEncoding.EncodeToString(key), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	plain := []byte("confidential source credential")
	encrypted, e := v.Seal(plain, "owner-a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Open(encrypted, "owner-b"); e == nil {
		t.Fatal("wrong owner decrypted secret")
	}
	decoded, e := v.Open(encrypted, "owner-a")
	if e != nil || !bytes.Equal(decoded, plain) {
		t.Fatal("roundtrip failed")
	}
	hash, e := v.Snapshot(plain)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(v.Dir, "snapshots", hash)
	body, _ := os.ReadFile(path)
	if bytes.Contains(body, plain) {
		t.Fatal("snapshot stored plaintext")
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm() != 0600 {
		t.Fatal("snapshot permissions not private")
	}
	got, e := v.ReadSnapshot(hash)
	if e != nil || !bytes.Equal(got, plain) {
		t.Fatal("snapshot integrity failed")
	}
	if _, e = v.ReadSnapshot("../../etc/passwd"); e == nil {
		t.Fatal("path traversal accepted")
	}
}
func TestCredentialsRejectedFromURLsAndDocuments(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "http://user:pass@example.com/", "http://example.com/?api_key=credential", "http://example.com/?code=credential", "http://example.com/?a=1;token=credential", "http://example.com/#token"} {
		if SafeURL(u) == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if SafeURL("https://example.com/feed?page=2") != nil {
		t.Fatal("ordinary query rejected")
	}
	if ValidateDocument([]byte(`{"headers":{"Cookie":"private"}}`)) == nil {
		t.Fatal("embedded secret accepted")
	}
	if ValidateDocument([]byte(`{"sites":[{"key":"site-id"}]}`)) != nil {
		t.Fatal("TVBox key is not a credential")
	}
	if ValidateHeaders(map[string]string{"X-Test": "ok\r\nAuthorization: secret"}) == nil {
		t.Fatal("header injection accepted")
	}
	if ValidateHeaders(map[string]string{"Host": "metadata"}) == nil {
		t.Fatal("Host override accepted")
	}
	if strings.Contains(Token(), "=") {
		t.Fatal("token must be URL safe")
	}
}
