// Package fetch is the only outbound HTTP boundary. It validates DNS answers at dial time,
// checks every redirect, bounds time/body size and never inherits process proxy settings.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/Cylunex/shadow-relay/internal/security"
)

var ErrBlocked = errors.New("target blocked by network policy")

type Policy struct {
	Network string
	Trust   string
}
type Result struct {
	Body                            []byte
	Status                          int
	ETag, LastModified, ContentType string
}
type hostState struct {
	slots    chan struct{}
	failures int
	until    time.Time
}
type Fetcher struct {
	Trusted []netip.Prefix
	mu      sync.Mutex
	hosts   map[string]*hostState
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
}

func New(cidrs string) (*Fetcher, error) {
	f := &Fetcher{hosts: map[string]*hostState{}, lookup: net.DefaultResolver.LookupNetIP, dial: (&net.Dialer{Timeout: 8 * time.Second}).DialContext}
	for _, s := range strings.Split(cidrs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, e := netip.ParsePrefix(s)
		if e != nil {
			return nil, errors.New("invalid trusted CIDR")
		}
		f.Trusted = append(f.Trusted, p)
	}
	return f, nil
}
func (f *Fetcher) Allowed(ip netip.Addr, p Policy) bool {
	ip = ip.Unmap()
	for _, raw := range []string{"0.0.0.0/8", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsPrivate() || !ip.IsGlobalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		if p.Network != "trusted-lan" || p.Trust != "trusted" {
			return false
		}
		for _, n := range f.Trusted {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	if p.Network == "trusted-lan" {
		for _, n := range f.Trusted {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	return p.Network == "internet" || p.Network == ""
}
func (f *Fetcher) client(p Policy) *http.Client {
	tr := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, ErrBlocked
		}
		ips, e := f.lookup(ctx, "ip", host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("DNS lookup failed")
		}
		for _, ip := range ips {
			if !f.Allowed(ip, p) {
				return nil, ErrBlocked
			}
		}
		var conn net.Conn
		for _, ip := range ips {
			conn, e = f.dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("connection failed")
	}}
	return &http.Client{Transport: &gatedTransport{f: f, base: tr}, Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if e := security.SafeURL(req.URL.String()); e != nil {
			return e
		}
		if req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
			req.Header = make(http.Header)
		}
		return nil
	}}
}
func (f *Fetcher) Get(ctx context.Context, raw string, p Policy, headers map[string]string, limit int64, partial bool) (out Result, err error) {
	if e := security.SafeURL(raw); e != nil {
		return out, e
	}
	if e := security.ValidateHeaders(headers); e != nil {
		return out, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return out, errors.New("invalid request")
	}
	req.Header.Set("User-Agent", "Shadow-Relay/0.1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if partial {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	}
	resp, e := f.client(p).Do(req)
	if e != nil {
		return out, errors.New("upstream request failed (network, TLS or policy)")
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	out.ETag = resp.Header.Get("ETag")
	out.LastModified = resp.Header.Get("Last-Modified")
	out.ContentType = resp.Header.Get("Content-Type")
	if resp.StatusCode == 304 {
		return out, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return out, errors.New("compressed response is not accepted")
	}
	max := limit + 1
	if partial {
		max = limit
	}
	out.Body, e = io.ReadAll(io.LimitReader(resp.Body, max))
	if e != nil {
		return out, errors.New("upstream body read failed")
	}
	if !partial && int64(len(out.Body)) > limit {
		return out, errors.New("upstream body too large")
	}
	if len(out.Body) == 0 {
		return out, errors.New("empty upstream body")
	}
	return out, nil
}

// Every redirect acquires the destination host's budget. A slot remains held until its body closes.
type gatedTransport struct {
	f    *Fetcher
	base http.RoundTripper
}
type gatedBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (b *gatedBody) Close() error { e := b.ReadCloser.Close(); b.once.Do(b.release); return e }
func (g *gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	g.f.mu.Lock()
	h := g.f.hosts[req.URL.Hostname()]
	if h == nil {
		h = &hostState{slots: make(chan struct{}, 2)}
		g.f.hosts[req.URL.Hostname()] = h
	}
	blocked := time.Now().Before(h.until)
	g.f.mu.Unlock()
	if blocked {
		return nil, errors.New("host circuit is open")
	}
	select {
	case h.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	res, e := g.base.RoundTrip(req)
	g.f.mu.Lock()
	if e != nil || res.StatusCode >= 500 {
		h.failures++
		if h.failures >= 3 {
			h.until = time.Now().Add(time.Minute)
		}
	} else {
		h.failures = 0
		h.until = time.Time{}
	}
	g.f.mu.Unlock()
	if e != nil {
		<-h.slots
		return nil, e
	}
	res.Body = &gatedBody{ReadCloser: res.Body, release: func() { <-h.slots }}
	return res, nil
}
