package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// ResolveMusic refreshes a playable URL for lx-music / music-playlist sources.
// LX Music scripts execute in a compatible client; Relay returns descriptor + vaulted
// credential hints so the client (or a future OpenSubsonic runtime) can resolve play URLs.
func (s *Service) ResolveMusic(ctx context.Context, sourceID, query, songID string) (map[string]any, error) {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", sourceID)
	if e != nil {
		return nil, e
	}
	if src.Protocol != "lx-music" && src.Protocol != "music-playlist" {
		return nil, errors.New("music resolve requires lx-music or music-playlist")
	}
	if !src.Enabled || src.ActiveRevision == "" {
		return nil, errors.New("source is not live")
	}
	rev, e := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", src.ActiveRevision)
	if e != nil {
		return nil, e
	}
	headers, _ := s.Headers(ctx, src.ID)
	out := map[string]any{
		"sourceId": src.ID, "protocol": src.Protocol, "query": strings.TrimSpace(query), "songId": strings.TrimSpace(songID),
		"capabilities": src.Capabilities,
		"note": "Relay stores descriptors and vaults secrets; search/resolve/play URL refresh runs in a compatible LX client or OpenSubsonic runtime",
	}
	if src.Protocol == "lx-music" {
		var cfg map[string]any
		_ = json.Unmarshal(rev.Normalized.Config, &cfg)
		out["descriptor"] = cfg
		out["hasVaultCredentials"] = len(headers) > 0
		out["playUrl"] = nil
		out["status"] = "client-resolve-required"
		return out, nil
	}
	// music-playlist: match by id/name in normalized items
	for _, item := range rev.Normalized.Items {
		if songID != "" && (item.ID == songID || item.URL == songID) {
			out["playUrl"] = item.URL
			out["title"] = item.Name
			out["status"] = "ok"
			return out, nil
		}
		if query != "" && strings.Contains(strings.ToLower(item.Name), strings.ToLower(query)) {
			out["playUrl"] = item.URL
			out["title"] = item.Name
			out["status"] = "ok"
			return out, nil
		}
	}
	out["playUrl"] = nil
	out["status"] = "not_found"
	return out, nil
}
