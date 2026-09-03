package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/service"
	"github.com/Cylunex/shadow-relay/internal/transfer"
)

func uploadRequest(t *testing.T, data []byte, mode, token, origin string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, err := form.CreateFormFile("file", "backup.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(data)
	if mode != "" {
		_ = form.WriteField("mode", mode)
	}
	_ = form.Close()
	r := httptest.NewRequest("POST", "/api/v1/data/import", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestDataTransferHTTPAuthenticationAndDefaultPreview(t *testing.T) {
	s, h := setup(t)
	_, err := s.Service.Import(context.Background(), service.Input{Name: "Transfer fixture", Content: "#EXTM3U\n#EXTINF:-1,News\nhttps://media.example.com/news.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "", "GET", "/api/v1/data/export", nil); w.Code != 401 {
		t.Fatal("anonymous export", w.Code)
	}
	exported := request(t, h, s.AdminToken, "GET", "/api/v1/data/export", nil)
	if exported.Code != 200 || exported.Header().Get("Content-Type") != "application/gzip" {
		t.Fatal(exported.Code, exported.Body.String())
	}
	for _, token := range []string{"", "invalid"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, uploadRequest(t, exported.Body.Bytes(), "apply", token, ""))
		if w.Code != 401 {
			t.Fatal("anonymous import", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, uploadRequest(t, exported.Body.Bytes(), "apply", s.AdminToken, "https://evil.example.com"))
	if w.Code != 403 {
		t.Fatal("cross-origin multipart import", w.Code)
	}
	var before, after int
	_ = s.Service.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM audits").Scan(&before)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, uploadRequest(t, exported.Body.Bytes(), "", s.AdminToken, ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var summary transfer.Summary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Applied || summary.KeyRequired || summary.Reused["sources"] != 1 {
		t.Fatal(summary)
	}
	_ = s.Service.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM audits").Scan(&after)
	if before != after {
		t.Fatal("preview wrote an audit")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, uploadRequest(t, []byte("not gzip"), "apply", s.AdminToken, ""))
	if w.Code != 400 {
		t.Fatal("invalid archive accepted", w.Code)
	}
}
