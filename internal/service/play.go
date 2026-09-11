package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// PlayResolveInput selects an item and optional upstream hint for resolve-then-redirect.
type PlayResolveInput struct {
	ItemID string `json:"itemId"`
	Query  string `json:"query"`
	URL    string `json:"url"` // optional already-known catalog URL to refresh via template
}

var (
	errPlayNotLive     = errors.New("source is not live")
	errPlayUnsupported = errors.New("protocol does not support play resolve; Emby/Jellyfin stay direct-client")
	errPlayNotFound    = errors.New("play item not found")
	errPlayNoDirect    = errors.New("redirect requires allowDirect without required play headers")
)

// ResolvePlay builds a PlaybackResource without streaming media bytes through Relay.
func (s *Service) ResolvePlay(ctx context.Context, sourceID string, in PlayResolveInput) (model.PlaybackResource, error) {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", sourceID)
	if e != nil {
		return model.PlaybackResource{}, e
	}
	if !src.Enabled || src.ActiveRevision == "" {
		return model.PlaybackResource{}, errPlayNotLive
	}
	// Explicit: media services talk client→Emby/Jellyfin; Relay is not in the stream path.
	if slices.Contains([]string{"emby", "jellyfin"}, src.Protocol) {
		return model.PlaybackResource{}, errPlayUnsupported
	}
	rev, e := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", src.ActiveRevision)
	if e != nil {
		return model.PlaybackResource{}, e
	}
	refresh := "/api/v1/sources/" + src.ID + "/play/resolve"
	base := model.PlaybackResource{
		SourceID:    src.ID,
		Protocol:    src.Protocol,
		RefreshPath: refresh,
		Status:      "ok",
	}
	switch src.Protocol {
	case "music-playlist", "m3u":
		return s.resolveCompiledItem(ctx, src, rev, in, base)
	case "lx-music":
		base.MustUseRuntime = true
		base.AllowDirect = false
		base.Status = "client-resolve-required"
		base.Note = "LX Music scripts run in a compatible client; Relay does not execute them or proxy audio bytes"
		base.ItemID = strings.TrimSpace(in.ItemID)
		return base, nil
	case "direct-link":
		return s.resolveDirectLink(ctx, src, rev, in, base)
	case "tvbox":
		// Safe TVBox sites are CMS APIs; play URL parse is deferred. Compiled live lists can resolve like m3u.
		return s.resolveTVBoxPlay(ctx, src, rev, in, base)
	default:
		return model.PlaybackResource{}, fmt.Errorf("play resolve not implemented for protocol %s", src.Protocol)
	}
}

func (s *Service) resolveCompiledItem(ctx context.Context, src model.Source, rev model.Revision, in PlayResolveInput, out model.PlaybackResource) (model.PlaybackResource, error) {
	item, ok := findPlayItem(rev.Normalized.Items, in)
	if !ok {
		out.Status = "not_found"
		out.AllowDirect = false
		return out, errPlayNotFound
	}
	headers, _ := s.Headers(ctx, src.ID)
	playHeaders := filterPlayHeaders(headers)
	if e := s.validatePlayTarget(ctx, item.URL, src); e != nil {
		return out, e
	}
	out.ItemID = firstNonEmpty(item.ID, item.URL)
	out.Title = item.Name
	out.URL = item.URL
	out.Headers = playHeaders
	out.AllowDirect = len(playHeaders) == 0
	out.MustUseRuntime = false
	out.Status = "ok"
	out.Note = "compiled playlist URL; client fetches media directly (Relay does not proxy bytes)"
	return out, nil
}

func (s *Service) resolveTVBoxPlay(ctx context.Context, src model.Source, rev model.Revision, in PlayResolveInput, out model.PlaybackResource) (model.PlaybackResource, error) {
	// Only resolve entries that already carry absolute play/list URLs (lives / store mirrors).
	// Executable parsers / spider play resolution stay deferred.
	item, ok := findPlayItem(rev.Normalized.Items, in)
	if !ok {
		out.Status = "not_found"
		out.MustUseRuntime = true
		out.AllowDirect = false
		out.Note = "TVBox CMS play-url parse is deferred; only compiled live/store HTTP URLs resolve here"
		return out, errPlayNotFound
	}
	if item.Group == "site" {
		out.MustUseRuntime = true
		out.AllowDirect = false
		out.Status = "runtime-backed"
		out.ItemID = firstNonEmpty(item.ID, item.Name)
		out.Title = item.Name
		out.Note = "TVBox HTTP CMS site requires client/runtime play parse; Relay does not reverse-proxy media"
		return out, nil
	}
	return s.resolveCompiledItem(ctx, src, rev, in, out)
}

func (s *Service) resolveDirectLink(ctx context.Context, src model.Source, rev model.Revision, in PlayResolveInput, out model.PlaybackResource) (model.PlaybackResource, error) {
	var cfg map[string]any
	_ = json.Unmarshal(rev.Normalized.Config, &cfg)
	item, hasItem := findPlayItem(rev.Normalized.Items, in)
	headers, _ := s.Headers(ctx, src.ID)
	template := strAny(cfg["resolveTemplate"])
	if template == "" {
		template = strAny(cfg["resolveUrl"])
	}

	// Direct compiled URL on the item — no upstream round-trip.
	if hasItem && strings.TrimSpace(item.URL) != "" && template == "" {
		in2 := in
		if in2.ItemID == "" {
			in2.ItemID = item.ID
		}
		return s.resolveCompiledItem(ctx, src, rev, in2, out)
	}

	id := strings.TrimSpace(in.ItemID)
	if id == "" && hasItem {
		id = item.ID
	}
	query := strings.TrimSpace(in.Query)
	hintURL := strings.TrimSpace(in.URL)
	if hintURL == "" && hasItem {
		hintURL = item.URL
	}

	if template == "" {
		if hasItem && item.URL != "" {
			return s.resolveCompiledItem(ctx, src, rev, in, out)
		}
		out.Status = "not_found"
		return out, errPlayNotFound
	}

	resolveURL, e := expandResolveTemplate(template, id, query, hintURL)
	if e != nil {
		return out, e
	}
	// Resolve endpoint itself must be a safe subscription-style URL (no embedded secrets in template query).
	if e := security.SafeURL(resolveURL); e != nil {
		return out, fmt.Errorf("resolve template URL: %w", e)
	}
	policy := fetch.Policy{Network: src.Network, Trust: src.Trust, ProxyID: src.ProxyID}
	// Small JSON/text only — never pull media bodies through Relay.
	res, e := s.Fetch.Get(ctx, resolveURL, policy, headers, 64<<10, false)
	if e != nil {
		return out, fmt.Errorf("upstream resolve failed: %w", e)
	}
	playURL, playHeaders, expires, e := parseResolveBody(res.Body, res.ContentType)
	if e != nil {
		return out, e
	}
	if e := s.validatePlayTarget(ctx, playURL, src); e != nil {
		return out, e
	}
	// Play headers come only from the upstream resolve JSON — vault secrets are for the
	// resolve request itself and must not be forced into Location or client play headers.
	if len(playHeaders) > 0 {
		if e := security.ValidateHeaders(playHeaders); e != nil {
			return out, e
		}
	}
	out.ItemID = id
	if hasItem {
		out.Title = item.Name
	}
	out.URL = playURL
	out.Headers = playHeaders
	out.ExpiresAt = expires
	out.AllowDirect = len(playHeaders) == 0
	out.MustUseRuntime = false
	out.Status = "ok"
	out.Note = "resolved upstream play URL; client must fetch media directly"
	return out, nil
}

func (s *Service) validatePlayTarget(ctx context.Context, raw string, src model.Source) error {
	if s.Fetch == nil {
		return security.SafePlayURL(raw)
	}
	return s.Fetch.ValidatePlayURL(ctx, raw, fetch.Policy{Network: src.Network, Trust: src.Trust, ProxyID: src.ProxyID})
}

func findPlayItem(items []model.Item, in PlayResolveInput) (model.Item, bool) {
	id := strings.TrimSpace(in.ItemID)
	q := strings.TrimSpace(in.Query)
	u := strings.TrimSpace(in.URL)
	for _, item := range items {
		if id != "" && (item.ID == id || item.URL == id || item.Name == id) {
			return item, true
		}
		if u != "" && item.URL == u {
			return item, true
		}
	}
	if q != "" {
		lq := strings.ToLower(q)
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Name), lq) {
				return item, true
			}
		}
	}
	return model.Item{}, false
}

func expandResolveTemplate(template, id, query, hintURL string) (string, error) {
	replacer := strings.NewReplacer(
		"{id}", url.QueryEscape(id),
		"{query}", url.QueryEscape(query),
		"{url}", url.QueryEscape(hintURL),
	)
	out := replacer.Replace(template)
	if strings.Contains(out, "{") && strings.Contains(out, "}") {
		return "", errors.New("resolve template has unresolved placeholders")
	}
	return out, nil
}

func parseResolveBody(body []byte, contentType string) (playURL string, headers map[string]string, expires string, err error) {
	trim := strings.TrimSpace(string(body))
	if trim == "" {
		return "", nil, "", errors.New("empty resolve response")
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "json") || strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") {
		var obj map[string]any
		if e := json.Unmarshal(body, &obj); e != nil {
			return "", nil, "", errors.New("resolve response is not valid JSON")
		}
		playURL = firstNonEmpty(strAny(obj["url"]), strAny(obj["playUrl"]), strAny(obj["play_url"]), strAny(obj["location"]))
		expires = firstNonEmpty(strAny(obj["expiresAt"]), strAny(obj["expires_at"]), strAny(obj["expires"]))
		if h, ok := obj["headers"].(map[string]any); ok {
			headers = map[string]string{}
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
		if playURL == "" {
			return "", nil, "", errors.New("resolve JSON missing url/playUrl")
		}
		return playURL, headers, expires, nil
	}
	// Plain-text absolute URL
	if strings.HasPrefix(trim, "http://") || strings.HasPrefix(trim, "https://") {
		line := strings.Split(trim, "\n")[0]
		return strings.TrimSpace(line), nil, "", nil
	}
	return "", nil, "", errors.New("unsupported resolve response")
}

func filterPlayHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	// Only forward headers that players commonly need; never invent Authorization into Location.
	out := map[string]string{}
	for k, v := range h {
		lk := strings.ToLower(k)
		if lk == "referer" || lk == "user-agent" || lk == "origin" || lk == "authorization" || lk == "cookie" || strings.HasPrefix(lk, "x-") {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func strAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// CanRedirect reports whether format=redirect may emit a 302 Location.
func CanRedirect(p model.PlaybackResource) error {
	if p.Status != "ok" || !p.AllowDirect || p.MustUseRuntime || p.URL == "" {
		return errPlayNoDirect
	}
	if len(p.Headers) > 0 {
		return errPlayNoDirect
	}
	if e := security.SafePlayURL(p.URL); e != nil {
		return e
	}
	return nil
}

// ResolveMusic keeps the legacy map shape while delegating to ResolvePlay for playlist URLs.
func (s *Service) ResolveMusic(ctx context.Context, sourceID, query, songID string) (map[string]any, error) {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", sourceID)
	if e != nil {
		return nil, e
	}
	if src.Protocol != "lx-music" && src.Protocol != "music-playlist" {
		return nil, errors.New("music resolve requires lx-music or music-playlist")
	}
	res, e := s.ResolvePlay(ctx, sourceID, PlayResolveInput{ItemID: songID, Query: query})
	if e != nil && !errors.Is(e, errPlayNotFound) {
		// lx-music returns ok descriptor without error
		if src.Protocol != "lx-music" {
			return nil, e
		}
	}
	out := map[string]any{
		"sourceId": res.SourceID, "protocol": res.Protocol, "query": strings.TrimSpace(query), "songId": strings.TrimSpace(songID),
		"capabilities":   src.Capabilities,
		"playUrl":        nil,
		"status":         res.Status,
		"allowDirect":    res.AllowDirect,
		"mustUseRuntime": res.MustUseRuntime,
		"refreshPath":    res.RefreshPath,
	}
	if res.URL != "" {
		out["playUrl"] = res.URL
	}
	if res.Title != "" {
		out["title"] = res.Title
	}
	if res.Note != "" {
		out["note"] = res.Note
	}
	if src.Protocol == "lx-music" {
		rev, e := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", src.ActiveRevision)
		if e == nil {
			var cfg map[string]any
			_ = json.Unmarshal(rev.Normalized.Config, &cfg)
			out["descriptor"] = cfg
		}
		headers, _ := s.Headers(ctx, src.ID)
		out["hasVaultCredentials"] = len(headers) > 0
		out["note"] = "Relay stores descriptors and vaults secrets; search/resolve/play URL refresh runs in a compatible LX client or OpenSubsonic runtime"
		out["status"] = "client-resolve-required"
		out["playUrl"] = nil
	}
	if res.Status == "not_found" {
		out["status"] = "not_found"
		out["playUrl"] = nil
	}
	return out, nil
}
