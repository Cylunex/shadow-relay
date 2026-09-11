package fetch

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"

	"github.com/Cylunex/shadow-relay/internal/security"
)

// ValidatePlayURL ensures a resolve-then-redirect Location is absolute HTTP(S)
// and every DNS answer is allowed by the outbound policy (no open redirect to private nets).
// It does not fetch the body — media bytes must not transit Relay.
func (f *Fetcher) ValidatePlayURL(ctx context.Context, raw string, p Policy) error {
	if e := security.SafePlayURL(raw); e != nil {
		return e
	}
	u, e := url.Parse(raw)
	if e != nil {
		return errors.New("invalid play URL")
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if !f.Allowed(ip, p) {
			return ErrBlocked
		}
		return nil
	}
	lookup := f.lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	ips, e := lookup(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return errors.New("play URL DNS lookup failed")
	}
	for _, ip := range ips {
		if !f.Allowed(ip, p) {
			return ErrBlocked
		}
	}
	return nil
}
