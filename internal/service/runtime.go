package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

type Connector struct {
	Name         string   `json:"name"`
	StatusPath   string   `json:"statusPath"`
	StatePath    string   `json:"statePath"`
	RequiredKey  string   `json:"requiredKey"`
	Protocols    []string `json:"protocols"`
	MediaTypes   []string `json:"mediaTypes"`
	Capabilities []string `json:"capabilities"`
	ProbeLevel   string   `json:"probeLevel"`
}

var Connectors = map[string]Connector{
	"emby":           {Name: "Emby", StatusPath: "/System/Info/Public", StatePath: "/Library/VirtualFolders", RequiredKey: "Version", Protocols: []string{"emby"}, MediaTypes: []string{"video.movie", "video.series", "audio.music"}, Capabilities: []string{"browse", "search", "stream", "progress"}, ProbeLevel: "service"},
	"jellyfin":       {Name: "Jellyfin", StatusPath: "/System/Info/Public", StatePath: "/Library/VirtualFolders", RequiredKey: "Version", Protocols: []string{"jellyfin"}, MediaTypes: []string{"video.movie", "video.series", "audio.music"}, Capabilities: []string{"browse", "search", "stream", "progress"}, ProbeLevel: "service"},
	"dispatcharr":    {Name: "Dispatcharr", StatusPath: "/api/core/version/", StatePath: "/api/channels/channels/", RequiredKey: "version", Protocols: []string{"dispatcharr", "m3u", "xmltv"}, MediaTypes: []string{"video.live", "support.epg"}, Capabilities: []string{"live", "epg", "catchup"}, ProbeLevel: "service"},
	"legado-hub":     {Name: "LegadoHub", StatusPath: "/api/auth/entrypoint", StatePath: "/api/auth/entrypoint", RequiredKey: "entrypoint", Protocols: []string{"legado-hub", "legado-book", "so-novel", "relay-book"}, MediaTypes: []string{"text.novel"}, Capabilities: []string{"search", "detail", "chapter", "progress"}, ProbeLevel: "service"},
	"suwayomi":       {Name: "Suwayomi", StatusPath: "/api/v1/settings/about", StatePath: "/api/v1/source/list", RequiredKey: "name", Protocols: []string{"suwayomi", "mihon-repo"}, MediaTypes: []string{"image.comic"}, Capabilities: []string{"browse", "search", "page", "download", "progress"}, ProbeLevel: "service"},
	"audiobookshelf": {Name: "Audiobookshelf", StatusPath: "/api/libraries", StatePath: "/api/libraries", RequiredKey: "libraries", Protocols: []string{"audiobookshelf"}, MediaTypes: []string{"audio.audiobook", "audio.podcast"}, Capabilities: []string{"browse", "search", "stream", "progress"}, ProbeLevel: "authenticated-state"},
	"miniflux":       {Name: "Miniflux", StatusPath: "/v1/me", StatePath: "/v1/feeds", RequiredKey: "id", Protocols: []string{"miniflux", "rss", "atom", "json-feed", "opml"}, MediaTypes: []string{"text.article"}, Capabilities: []string{"browse", "favorite", "progress"}, ProbeLevel: "authenticated-state"},
}

func resolveURL(base, reference string) string {
	b, e := url.Parse(base)
	if e != nil {
		return reference
	}
	r, e := url.Parse(reference)
	if e != nil {
		return reference
	}
	return b.ResolveReference(r).String()
}
func validateRuntimeResponse(c Connector, b []byte) (map[string]any, error) {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m[c.RequiredKey] == nil {
		return nil, errors.New("runtime returned an incompatible API response")
	}
	return m, nil
}
func (s *Service) SaveRuntime(ctx context.Context, id string, in model.Runtime, h map[string]string) (model.Runtime, error) {
	if e := requireName(in.Name); e != nil {
		return in, e
	}
	c, ok := Connectors[in.Driver]
	if !ok {
		return in, errors.New("unsupported runtime driver")
	}
	if e := security.SafeURL(in.URL); e != nil {
		return in, e
	}
	defaultsIn := Input{Name: in.Name, Network: in.Network, Trust: in.Trust}
	if e := defaults(&defaultsIn); e != nil {
		return in, e
	}
	in.Network = defaultsIn.Network
	in.Trust = defaultsIn.Trust
	in.Capabilities = c.Capabilities
	in.Health = "unknown"
	in.ID = id
	if id == "" {
		in.ID = model.ID("runtime")
	}
	in.Version = ""
	in.LastChecked = ""
	in.LastSync = ""
	in.State = nil
	in.UpdatedAt = model.Now()
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if id != "" {
			old, e := store.Get[model.Runtime](ctx, tx, "runtimes", id)
			if e != nil {
				return e
			}
			if old.Driver != in.Driver {
				return errors.New("runtime driver is immutable")
			}
		}
		if e := store.Put(ctx, tx, "runtimes", in.ID, in); e != nil {
			return e
		}
		if e := s.saveSecret(ctx, tx, in.ID, h); e != nil {
			return e
		}
		return audit(ctx, tx, "runtime.save", in.ID)
	})
	return in, e
}
func (s *Service) TestRuntime(ctx context.Context, id string, syncState bool) error {
	rt, e := store.Get[model.Runtime](ctx, s.DB.Pool, "runtimes", id)
	if e != nil {
		return e
	}
	c, ok := Connectors[rt.Driver]
	if !ok {
		return errors.New("unsupported runtime")
	}
	h, e := s.Headers(ctx, id)
	if e != nil {
		return e
	}
	res, err := s.Fetch.Get(ctx, strings.TrimRight(rt.URL, "/")+c.StatusPath, fetch.Policy{Network: rt.Network, Trust: rt.Trust}, h, 2<<20, false)
	var status map[string]any
	if err == nil {
		status, err = validateRuntimeResponse(c, res.Body)
	}
	summary := map[string]any{"probeLevel": c.ProbeLevel, "configSync": "pull-only"}
	if err == nil && syncState {
		var b []byte
		if c.StatePath == c.StatusPath {
			b = res.Body
		} else {
			r, e := s.Fetch.Get(ctx, strings.TrimRight(rt.URL, "/")+c.StatePath, fetch.Policy{Network: rt.Network, Trust: rt.Trust}, h, 4<<20, false)
			err = e
			b = r.Body
		}
		if err == nil {
			var v any
			if json.Unmarshal(b, &v) != nil {
				err = errors.New("invalid runtime state JSON")
			} else {
				switch v := v.(type) {
				case []any:
					summary["itemCount"] = len(v)
				case map[string]any:
					for _, k := range []string{"libraries", "results", "sources"} {
						if a, ok := v[k].([]any); ok {
							summary["itemCount"] = len(a)
						}
					}
				}
			}
		}
	}
	e = s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Runtime](ctx, tx, "runtimes", id)
		if e != nil {
			return e
		}
		if v.URL != rt.URL || v.Driver != rt.Driver || v.UpdatedAt != rt.UpdatedAt {
			return ErrConflict
		}
		v.LastChecked = model.Now()
		v.Health = "healthy"
		if err != nil {
			v.Health = "failing"
		} else {
			for _, key := range []string{"Version", "version"} {
				if value, ok := status[key].(string); ok && len(value) < 100 {
					v.Version = value
				}
			}
			v.State = jsonBytes(summary)
			if syncState {
				v.LastSync = model.Now()
			}
		}
		if e = store.Put(ctx, tx, "runtimes", id, v); e != nil {
			return e
		}
		return audit(ctx, tx, "runtime.test", id)
	})
	if e != nil {
		return e
	}
	return err
}
