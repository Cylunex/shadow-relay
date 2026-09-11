package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/service"
)

type playStubFetch struct {
	blockedHost string
	getBody     []byte
	lastGet     string
}

func (f *playStubFetch) Get(_ context.Context, u string, _ fetch.Policy, headers map[string]string, _ int64, _ bool) (fetch.Result, error) {
	f.lastGet = u
	for k := range headers {
		if security.SensitiveKey(k) && strings.Contains(u, headers[k]) {
			return fetch.Result{}, fetch.ErrBlocked
		}
	}
	if f.getBody != nil {
		return fetch.Result{Status: 200, Body: f.getBody, ContentType: "application/json"}, nil
	}
	return fetch.Result{}, fetch.ErrBlocked
}
func (f *playStubFetch) ValidatePlayURL(_ context.Context, raw string, _ fetch.Policy) error {
	if e := security.SafePlayURL(raw); e != nil {
		return e
	}
	if strings.Contains(raw, "127.0.0.1") || strings.Contains(raw, "localhost") || (f.blockedHost != "" && strings.Contains(raw, f.blockedHost)) {
		return fetch.ErrBlocked
	}
	return nil
}

func TestPlayResolveJSONAndRedirect(t *testing.T) {
	s, h := setup(t)
	stub := &playStubFetch{}
	s.Service.Fetch = stub
	ctx := context.Background()
	body := "#EXTM3U\n#EXTINF:-1,Song A\nhttps://music.example.com/a.mp3\n#EXTINF:-1,Song B\nhttps://music.example.com/b.flac\n"
	src, err := s.Service.Import(ctx, service.Input{Name: "Mix", Content: body, Protocol: "music-playlist"})
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, h, s.AdminToken, "POST", "/api/v1/sources/"+src.ID+"/play/resolve", map[string]string{"query": "Song B"})
	if w.Code != 200 {
		t.Fatalf("json resolve %d %s", w.Code, w.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["url"] != "https://music.example.com/b.flac" || res["allowDirect"] != true {
		t.Fatalf("%v", res)
	}
	if strings.Contains(w.Body.String(), "Authorization") && strings.Contains(w.Body.String(), "secret") {
		t.Fatal("credential leak in JSON")
	}

	r := httptest.NewRequest("GET", "/api/v1/sources/"+src.ID+"/play/resolve?query=Song%20B&format=redirect", nil)
	r.Header.Set("Authorization", "Bearer "+s.AdminToken)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r)
	if w2.Code != http.StatusFound {
		t.Fatalf("redirect %d %s", w2.Code, w2.Body.String())
	}
	loc := w2.Header().Get("Location")
	if loc != "https://music.example.com/b.flac" {
		t.Fatalf("location %q", loc)
	}
}

func TestPlayResolveRedirectRejectsPrivate(t *testing.T) {
	s, h := setup(t)
	stub := &playStubFetch{}
	s.Service.Fetch = stub
	ctx := context.Background()
	// Craft source with public import then we need private URL — SafeURL allows 127.0.0.1
	src, err := s.Service.Import(ctx, service.Input{Name: "Bad", Content: "#EXTM3U\n#EXTINF:-1,Bad\nhttp://127.0.0.1/secret.ts\n"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/sources/"+src.ID+"/play/resolve?query=Bad&format=redirect", nil)
	r.Header.Set("Authorization", "Bearer "+s.AdminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusFound {
		t.Fatalf("private redirect emitted Location=%s", w.Header().Get("Location"))
	}
	if strings.Contains(w.Body.String(), "secret.ts") || strings.Contains(w.Header().Get("Location"), "127.0.0.1") {
		// error JSON should not need to echo full target; Location must be empty
		if w.Header().Get("Location") != "" {
			t.Fatal("Location set for blocked target")
		}
	}
}

func TestPlayResolveDirectLinkNoCredentialInLocation(t *testing.T) {
	s, h := setup(t)
	body, _ := json.Marshal(map[string]any{"url": "https://cdn.example.com/f.mp4?signature=sig", "headers": map[string]string{}})
	stub := &playStubFetch{getBody: body}
	s.Service.Fetch = stub
	ctx := context.Background()
	doc := `{"schema":"shadow.direct-link/v1","resolveTemplate":"https://api.example.com/r?id={id}","items":[{"id":"f1","name":"File"}]}`
	src, err := s.Service.Import(ctx, service.Input{Name: "DL", Content: doc, Headers: map[string]string{"Authorization": "vault-secret-value"}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/sources/"+src.ID+"/play/resolve?itemId=f1&format=redirect", nil)
	r.Header.Set("Authorization", "Bearer "+s.AdminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "vault-secret-value") || strings.Contains(stub.lastGet, "vault-secret-value") {
		t.Fatalf("credential leaked loc=%q get=%q", loc, stub.lastGet)
	}
	if !strings.HasPrefix(loc, "https://cdn.example.com/f.mp4") {
		t.Fatalf("loc %q", loc)
	}
}
