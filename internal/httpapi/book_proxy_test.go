package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/fetch"
)

const apiBook = `{"bookSourceName":"API test book","bookSourceUrl":"https://books.example.com","searchUrl":"/search?q={{key}}","ruleSearch":{"bookList":".book","name":"a@text","bookUrl":"a@href"},"ruleBookInfo":{"name":"h1@text"},"ruleToc":{"chapterList":".toc a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":"#content@html"}}`

func TestBookSourceProxyAPI(t *testing.T) {
	s, h := setup(t)
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "8.8.8.8:80" {
			t.Errorf("unvalidated proxy target: %s %s", r.Method, r.Host)
			return
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		rw.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		rw.Flush()
		req, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		if req.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached origin")
		}
		requests.Add(1)
		fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(apiBook), apiBook)
		rw.Flush()
	}))
	defer proxy.Close()
	config, _ := json.Marshal(map[string]string{"books": strings.Replace(proxy.URL, "://", "://user:private-proxy-password@", 1)})
	f, err := fetch.NewWithProxies("", string(config))
	if err != nil {
		t.Fatal(err)
	}
	s.Service.Fetch = f
	call := func(method, path string, in any, status int) []byte {
		t.Helper()
		w := request(t, h, s.AdminToken, method, path, in)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-proxy-password") || strings.Contains(w.Body.String(), proxy.URL) {
			t.Fatal("proxy configuration leaked")
		}
		return w.Body.Bytes()
	}
	if w := request(t, h, "", "GET", "/api/v1/proxies", nil); w.Code != 401 {
		t.Fatal("profiles are not admin-only")
	}
	if string(call("GET", "/api/v1/proxies", nil, 200)) != "{\"proxyIds\":[\"books\"]}\n" {
		t.Fatal("wrong profile listing")
	}
	in := map[string]any{"name": "Book pack", "url": "http://8.8.8.8/books.json", "proxyId": "missing"}
	call("POST", "/api/v1/sources/preview", in, 400)
	if requests.Load() != 0 {
		t.Fatal("invalid profile made an outbound request")
	}
	in["proxyId"] = "books"
	call("POST", "/api/v1/sources/preview", in, 200)
	var src map[string]any
	json.Unmarshal(call("POST", "/api/v1/sources/import", in, 201), &src)
	id := src["id"].(string)
	if src["proxyId"] != "books" {
		t.Fatal("proxy not persisted")
	}
	path := "/api/v1/sources/" + id
	var before, after bookplugin.Report
	json.Unmarshal(call("GET", path+"/book-plugins", nil, 200), &before)
	in["hubProxyMode"] = "always"
	call("PUT", path, in, 200)
	json.Unmarshal(call("GET", path+"/book-plugins", nil, 200), &after)
	if before.Supported != 1 || after.Supported != 1 || after.Entries[0].Recipe.ProxyMode != "always" || bookplugin.Version(*before.Entries[0].Recipe) == bookplugin.Version(*after.Entries[0].Recipe) {
		t.Fatal("Hub route did not change plugin version")
	}
	archive := call("GET", path+"/hub.zip", nil, 200)
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range z.File {
		if strings.HasSuffix(file.Name, "metadata.yaml") {
			r, _ := file.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			var meta map[string]any
			json.Unmarshal(b, &meta)
			p := meta["proxy"].(map[string]any)
			if p["mode"] != "always" || p["required"] != true {
				t.Fatal("archive disabled requested proxy")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing plugin metadata")
	}
	delete(in, "proxyId")
	delete(in, "hubProxyMode")
	json.Unmarshal(call("PUT", path, in, 200), &src)
	if src["proxyId"] != "books" || src["hubProxyMode"] != "always" {
		t.Fatal("older client cleared proxy settings")
	}
	if err := s.Service.SyncSource(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatal("preview/import/sync did not all use source proxy")
	}
	in["hubProxyMode"] = "invalid"
	call("PUT", path, in, 400)
	in["hubProxyMode"] = "never"
	in["proxyId"] = ""
	call("PUT", path, in, 200)
	var direct map[string]any
	json.Unmarshal(call("GET", path, nil, 200), &direct)
	if direct["proxyId"] != nil {
		t.Fatal("explicit clear failed")
	}
	call("DELETE", path, nil, 200)
}
