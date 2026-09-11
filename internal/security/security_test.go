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
	if ValidateDocument([]byte(`[{"bookSourceName":"demo","bookSourceUrl":"https://example.com/##note","searchUrl":"https://example.com/s?q={{key}},{\n  \"charset\": \"utf-8\"\n}"}]`)) != nil {
		t.Fatal("Legado opaque rule URLs with ## or comma-JSON must be accepted")
	}
	if ValidateDocument([]byte(`{"url":"http://user:pass@example.com/"}`)) == nil {
		t.Fatal("userinfo in embedded URL accepted")
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

func TestSafePlayURLAllowsSignedQueryRejectsUserinfo(t *testing.T) {
	if SafePlayURL("https://cdn.example.com/a.mp4?signature=abc&Expires=123") != nil {
		t.Fatal("signed play URL should be allowed")
	}
	if SafePlayURL("https://cdn.example.com/a.mp4?token=secret") != nil {
		t.Fatal("token query on play URL should be allowed for CDN handoff")
	}
	if SafeURL("https://cdn.example.com/a.mp4?signature=abc") == nil {
		t.Fatal("SafeURL must still reject signature query on config URLs")
	}
	for _, u := range []string{"http://user:pass@cdn.example.com/a", "https://cdn.example.com/a#frag", "file:///etc/passwd", "javascript:alert(1)"} {
		if SafePlayURL(u) == nil {
			t.Errorf("accepted unsafe play URL %s", u)
		}
	}
	if RedactURL("https://cdn.example.com/path/a.mp4?signature=secret&token=x") != "https://cdn.example.com/path/a.mp4" {
		t.Fatalf("redact failed: %s", RedactURL("https://cdn.example.com/path/a.mp4?signature=secret&token=x"))
	}
}
