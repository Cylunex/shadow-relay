package fetch

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var proxyID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Proxy addresses are operator configuration, never source input or published data.
// Construct once at startup: profiles are immutable while requests are running.
func NewWithProxies(cidrs, config string) (*Fetcher, error) {
	f, err := New(cidrs)
	if err != nil {
		return nil, err
	}
	profiles := map[string]string{}
	if strings.TrimSpace(config) != "" {
		if json.Unmarshal([]byte(config), &profiles) != nil || profiles == nil || len(profiles) > 32 {
			return nil, errors.New("RELAY_PROXIES must be a JSON object with at most 32 HTTP(S) proxy profiles")
		}
	}
	f.proxies = map[string]*url.URL{}
	for id, raw := range profiles {
		u, err := url.Parse(raw)
		if err != nil || !proxyID.MatchString(id) || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\x00") {
			return nil, errors.New("invalid proxy profile; use a named HTTP(S) proxy URL without path, query or fragment")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return nil, errors.New("invalid proxy port")
			}
		}
		f.proxies[id] = u
	}
	return f, nil
}

func (f *Fetcher) ProxyIDs() []string {
	ids := make([]string, 0, len(f.proxies))
	for id := range f.proxies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (f *Fetcher) ValidateProxy(p Policy) error {
	if p.ProxyID == "" {
		return nil
	}
	if !proxyID.MatchString(p.ProxyID) || f.proxies[p.ProxyID] == nil {
		return errors.New("unknown proxy profile")
	}
	if p.Network != "internet" && p.Network != "" {
		return errors.New("proxy profiles are only available for internet sources")
	}
	return nil
}

// CONNECT to the already validated destination IP, including for plain HTTP.
// Letting a forward proxy resolve the original hostname would bypass DNS pinning.
// The proxy itself is an explicitly trusted operator endpoint and may be on LAN.
func (f *Fetcher) proxyDial(ctx context.Context, proxy *url.URL, target string) (net.Conn, error) {
	if proxy == nil {
		return nil, errors.New("unknown proxy profile")
	}
	port := proxy.Port()
	if port == "" {
		port = "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
	}
	c, err := f.dial(ctx, "tcp", net.JoinHostPort(proxy.Hostname(), port))
	if err != nil {
		return nil, errors.New("proxy unavailable")
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	if d, exists := ctx.Deadline(); exists && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	// Cancellation must interrupt both the TLS and CONNECT handshakes.
	raw := c
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { raw.Close(); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	if proxy.Scheme == "https" {
		t := tls.Client(c, &tls.Config{ServerName: proxy.Hostname(), MinVersion: tls.VersionTLS12})
		if err = t.HandshakeContext(ctx); err != nil {
			return nil, errors.New("proxy TLS failed")
		}
		c = t
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
	}
	if err = req.Write(c); err != nil {
		return nil, errors.New("proxy handshake failed")
	}
	// Bound untrusted CONNECT headers without imposing a limit on the tunnel.
	reader := bufio.NewReader(&headerReader{Reader: c, remaining: 16 << 10})
	resp, err := http.ReadResponse(reader, req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, errors.New("proxy CONNECT refused")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	ok = true
	return c, nil
}

type headerReader struct {
	Reader    net.Conn
	remaining int
}

func (r *headerReader) Read(b []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("proxy headers too large")
	}
	// Reading a byte at a time ensures no tunnel bytes are consumed by ReadResponse.
	n, err := r.Reader.Read(b[:1])
	r.remaining -= n
	return n, err
}
