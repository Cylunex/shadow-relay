package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/service"
	"github.com/Cylunex/shadow-relay/internal/testutil"
)

func setup(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	v, e := security.NewVault(base64.StdEncoding.EncodeToString(key), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	f, _ := fetch.New("")
	s := &Server{Service: &service.Service{DB: testutil.Database(t), Vault: v, Fetch: f}, AdminToken: security.Token(), PublicURL: "https://relay.example.com", WebDir: t.TempDir()}
	return s, s.Handler()
}
func request(t *testing.T, h http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthenticationStrictJSONAndOrigin(t *testing.T) {
	s, h := setup(t)
	for _, token := range []string{"", "incorrect"} {
		if w := request(t, h, token, "GET", "/api/v1/sources", nil); w.Code != 401 {
			t.Fatal("auth bypass", w.Code)
		}
	}
	if w := request(t, h, s.AdminToken, "GET", "/api/v1/sources", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, s.AdminToken, "POST", "/api/v1/sources/import", map[string]any{"name": "Bad", "unknown": true}); w.Code != 400 {
		t.Fatal("unknown JSON accepted")
	}
	r := httptest.NewRequest("POST", "/api/v1/sources/import", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+s.AdminToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	if w = request(t, h, "", "GET", "/healthz", nil); w.Code != 200 {
		t.Fatal("health unavailable")
	}
}
func TestHTTPPublicationRevocationBeforeConditionalGetAndNoSecretReadback(t *testing.T) {
	s, h := setup(t)
	ctx := context.Background()
	src, e := s.Service.Import(ctx, service.Input{Name: "Live", Content: "#EXTM3U\n#EXTINF:-1,News\nhttps://media.example.com/news.ts", Headers: map[string]string{"Authorization": "private-upstream"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range []string{"approve", "enable"} {
		if e = s.Service.SourceAction(ctx, src.ID, a, ""); e != nil {
			t.Fatal(e)
		}
	}
	set, e := s.Service.SaveSet(ctx, "", model.SourceSet{Name: "Home", Members: []model.Member{{SourceID: src.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Service.Publish(ctx, set.ID)
	if e != nil {
		t.Fatal(e)
	}
	b, token, e := s.Service.CreateBinding(ctx, model.Binding{Name: "Phone", SetID: set.ID, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	if e != nil {
		t.Fatal(e)
	}
	path := "/p/" + token + "/shadow.json"
	w := request(t, h, "", "GET", path, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-upstream") || strings.Contains(w.Body.String(), service.BasePlaceholder) {
		t.Fatal("publication leaked secret or unresolved placeholder")
	}
	if !strings.Contains(w.Body.String(), "https://relay.example.com/p/"+token+"/iptv/live.m3u") {
		t.Fatal("export URL missing")
	}
	etag := w.Header().Get("ETag")
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatal("conditional GET not supported")
	}
	w = request(t, h, "", "GET", "/p/"+token+"/v/"+p.ID+"/iptv/live.m3u", nil)
	if w.Code != 200 {
		t.Fatal("versioned export unavailable")
	}
	w = request(t, h, s.AdminToken, "GET", "/api/v1/secrets/"+src.ID, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-upstream") || !strings.Contains(w.Body.String(), "Authorization") {
		t.Fatal("secret metadata unsafe")
	}
	w = request(t, h, s.AdminToken, "GET", "/api/v1/bindings", nil)
	if strings.Contains(w.Body.String(), security.Hash([]byte(token))) || strings.Contains(w.Body.String(), token) {
		t.Fatal("binding token material readable")
	}
	_, e = s.Service.BindingAction(ctx, b.ID, "revoke")
	if e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("revoked token received cached 304")
	}
}
func TestRawSnapshotRequiresCorrectOwner(t *testing.T) {
	s, h := setup(t)
	ctx := context.Background()
	src, e := s.Service.Import(ctx, service.Input{Name: "One", Content: "#EXTM3U\n#EXTINF:-1,One\nhttps://media.example.com/one.ts"})
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/sources/" + src.ID + "/revisions/" + src.StagedRevision + "/raw"
	w := request(t, h, s.AdminToken, "GET", path, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "#EXTM3U") {
		t.Fatal("snapshot unavailable")
	}
	wrong := strings.Replace(path, src.ID, "wrong-source", 1)
	w = request(t, h, s.AdminToken, "GET", wrong, nil)
	if w.Code != 404 {
		t.Fatal("snapshot ownership not checked")
	}
	w = request(t, h, "", "GET", path, nil)
	if w.Code != 401 {
		t.Fatal("anonymous raw snapshot")
	}
}
