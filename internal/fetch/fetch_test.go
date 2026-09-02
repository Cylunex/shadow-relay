package fetch

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNetworkPolicy(t *testing.T) {
	f, _ := New("10.24.0.0/16,100.64.0.0/10")
	internet := Policy{Network: "internet", Trust: "trusted"}
	lan := Policy{Network: "trusted-lan", Trust: "trusted"}
	for _, ip := range []string{"127.0.0.1", "::1", "169.254.169.254", "::ffff:127.0.0.1", "192.168.1.1", "100.64.0.1", "10.24.1.2", "198.18.0.1", "64:ff9b::a00:1", "0.1.2.3"} {
		if f.Allowed(netip.MustParseAddr(ip), internet) {
			t.Errorf("internet permitted %s", ip)
		}
	}
	if !f.Allowed(netip.MustParseAddr("10.24.1.2"), lan) {
		t.Fatal("explicit trusted network blocked")
	}
	if f.Allowed(netip.MustParseAddr("10.25.1.2"), lan) {
		t.Fatal("unlisted LAN permitted")
	}
	if !f.Allowed(netip.MustParseAddr("8.8.8.8"), internet) {
		t.Fatal("public address blocked")
	}
}
func testFetcher(t *testing.T, h http.HandlerFunc) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	f, _ := New("")
	f.lookup = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "internal.example.com" {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	f.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(srv.URL, "http://"))
	}
	return f
}
func TestRedirectRechecksDNSAndStripsSecrets(t *testing.T) {
	var leaked atomic.Bool
	f := testFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "http://other.example.com/end", 302)
		case "/internal":
			http.Redirect(w, r, "http://internal.example.com/", 302)
		case "/end":
			if r.Header.Get("X-Custom-Secret") != "" {
				leaked.Store(true)
			}
			io.WriteString(w, "valid")
		}
	})
	res, e := f.Get(context.Background(), "http://public.example.com/start", Policy{}, map[string]string{"X-Custom-Secret": "private"}, 1024, false)
	if e != nil || string(res.Body) != "valid" || leaked.Load() {
		t.Fatal("redirect failed or credentials leaked", e)
	}
	if _, e = f.Get(context.Background(), "http://public.example.com/internal", Policy{}, nil, 1024, false); e == nil {
		t.Fatal("redirect to internal address permitted")
	}
}
func TestRejectMixedDNSAndBodyLimits(t *testing.T) {
	f := testFetcher(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 200)) })
	if _, e := f.Get(context.Background(), "http://public.example.com/", Policy{}, nil, 100, false); e == nil {
		t.Fatal("oversized body accepted")
	}
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}, nil
	}
	if _, e := f.Get(context.Background(), "http://public.example.com/", Policy{}, nil, 300, false); e == nil {
		t.Fatal("mixed public/private DNS allowed")
	}
}
