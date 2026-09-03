package fetch

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProxySelectionPinsDNSAndKeepsCredentialsInConnect(t *testing.T) {
	var connects, direct atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		if r.Method != "CONNECT" || r.Host != "8.8.8.8:80" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Errorf("unexpected CONNECT: %s %s", r.Method, r.Host)
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		req, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		if req.Host != "books.example.com" || req.Header.Get("Proxy-Authorization") != "" {
			t.Error("origin host changed or proxy credentials leaked")
		}
		body := "proxied"
		fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		rw.Flush()
	}))
	defer proxy.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); io.WriteString(w, "direct") }))
	defer upstream.Close()
	config, _ := json.Marshal(map[string]string{"books": strings.Replace(proxy.URL, "://", "://user:pass@", 1)})
	f, err := NewWithProxies("", string(config))
	if err != nil {
		t.Fatal(err)
	}
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	f.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "8.8.8.8:80" {
			addr = strings.TrimPrefix(upstream.URL, "http://")
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	t.Setenv("HTTP_PROXY", proxy.URL)
	for _, route := range []string{"", "books"} {
		res, err := f.Get(context.Background(), "http://books.example.com/", Policy{ProxyID: route}, nil, 100, false)
		want := "direct"
		if route != "" {
			want = "proxied"
		}
		if err != nil || string(res.Body) != want {
			t.Fatalf("route %q: %s %v", route, res.Body, err)
		}
	}
	if connects.Load() != 1 || direct.Load() != 1 {
		t.Fatal("source routing not isolated")
	}
	if _, err := f.Get(context.Background(), "http://books.example.com/", Policy{ProxyID: "missing"}, nil, 100, false); err == nil {
		t.Fatal("unknown proxy fell back to direct")
	}
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	if _, err := f.Get(context.Background(), "http://books.example.com/", Policy{ProxyID: "books"}, nil, 100, false); err == nil {
		t.Fatal("proxy bypassed mixed DNS check")
	}
	if connects.Load() != 1 {
		t.Fatal("blocked target reached proxy")
	}
}

func TestProxyRedirectCannotReachPrivateAddress(t *testing.T) {
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		c, rw, _ := w.(http.Hijacker).Hijack()
		defer c.Close()
		rw.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		rw.Flush()
		if _, err := http.ReadRequest(bufio.NewReader(c)); err != nil {
			return
		}
		io.WriteString(c, "HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1/private\r\nContent-Length: 0\r\n\r\n")
	}))
	defer proxy.Close()
	config, _ := json.Marshal(map[string]string{"books": proxy.URL})
	f, _ := NewWithProxies("", string(config))
	if _, err := f.Get(context.Background(), "http://8.8.8.8/", Policy{ProxyID: "books"}, nil, 100, false); err == nil {
		t.Fatal("private redirect accepted")
	}
	if connects.Load() != 1 {
		t.Fatal("redirect reached proxy")
	}
}

func TestProxyConfigurationValidationAndRedaction(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"bad id":"http://example.com"}`, `{"p":"socks5://example.com"}`, `{"p":"http://example.com:99999"}`, `{"p":"http://user:secret@example.com/path"}`} {
		_, err := NewWithProxies("", raw)
		if err == nil || strings.Contains(err.Error(), "user:secret") {
			t.Fatalf("invalid config accepted or leaked: %v", err)
		}
	}
	f, err := NewWithProxies("", `{"books":"http://user:secret@127.0.0.1:8888"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f.ValidateProxy(Policy{Network: "trusted-lan", ProxyID: "books"}) == nil {
		t.Fatal("LAN source accepted proxy")
	}
	if strings.Join(f.ProxyIDs(), ",") != "books" {
		t.Fatal("profile listing contains private configuration")
	}
}

func TestHTTPSOriginThroughProxyPreservesTLSIdentity(t *testing.T) {
	var leaked atomic.Bool
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.Header.Get("Proxy-Authorization") != "" {
			leaked.Store(true)
		}
		io.WriteString(w, "secure")
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "8.8.8.8:443" {
			t.Error("CONNECT did not pin HTTPS target")
			return
		}
		upstream, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "https://"))
		if err != nil {
			t.Error(err)
			return
		}
		defer upstream.Close()
		c, rw, _ := w.(http.Hijacker).Hijack()
		defer c.Close()
		rw.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		rw.Flush()
		done := make(chan struct{})
		go func() { io.Copy(upstream, c); upstream.Close(); close(done) }()
		io.Copy(c, upstream)
		c.Close()
		<-done
	}))
	defer proxy.Close()
	config, _ := json.Marshal(map[string]string{"books": proxy.URL})
	f, _ := NewWithProxies("", string(config))
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	client := f.client(Policy{ProxyID: "books"})
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	client.Transport.(*gatedTransport).base.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	response, err := client.Get("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	b, _ := io.ReadAll(response.Body)
	if string(b) != "secure" || leaked.Load() {
		t.Fatal("HTTPS identity or tunnel corrupted")
	}
}

func TestProxyRefusalNeverFallsBackToDirect(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", 407) }))
	defer proxy.Close()
	config, _ := json.Marshal(map[string]string{"books": proxy.URL})
	f, _ := NewWithProxies("", string(config))
	var direct atomic.Bool
	f.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != strings.TrimPrefix(proxy.URL, "http://") {
			direct.Store(true)
			return nil, fmt.Errorf("unexpected direct dial")
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	_, err := f.Get(context.Background(), "http://8.8.8.8/", Policy{ProxyID: "books"}, nil, 100, false)
	if err == nil || direct.Load() {
		t.Fatal("proxy failure fell back to direct")
	}
}
