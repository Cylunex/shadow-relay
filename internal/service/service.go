package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/adapter"
	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

type Service struct {
	DB    *store.DB
	Vault *security.Vault
	Fetch Fetcher
}

type Fetcher interface {
	Get(context.Context, string, fetch.Policy, map[string]string, int64, bool) (fetch.Result, error)
}

var ErrConflict = errors.New("configuration changed; refresh and retry")

type Input struct {
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Content         string            `json:"content"`
	Protocol        string            `json:"protocol"`
	Network         string            `json:"network"`
	Trust           string            `json:"trust"`
	Mode            string            `json:"mode"`
	RuntimeID       string            `json:"runtimeId"`
	MediaTypes      []string          `json:"mediaTypes"`
	UpdatePolicy    string            `json:"updatePolicy"`
	IntervalMinutes int               `json:"intervalMinutes"`
	Headers         map[string]string `json:"headers"`
}

func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func audit(ctx context.Context, tx *store.Tx, action, id string) error {
	a := model.Audit{ID: model.ID("audit"), Action: action, TargetID: id, CreatedAt: model.Now()}
	return store.Insert(ctx, tx, "audits", a.ID, a)
}
func defaults(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 160 {
		return errors.New("name must contain 1–160 characters")
	}
	if in.Network == "" {
		in.Network = "internet"
	}
	if in.Trust == "" {
		in.Trust = "reviewed"
	}
	if in.UpdatePolicy == "" {
		in.UpdatePolicy = "review"
	}
	if in.IntervalMinutes == 0 {
		in.IntervalMinutes = 360
	}
	if !slices.Contains([]string{"internet", "trusted-lan"}, in.Network) || !slices.Contains([]string{"untrusted", "reviewed", "trusted"}, in.Trust) {
		return errors.New("invalid network or trust")
	}
	if in.Network == "trusted-lan" && in.Trust != "trusted" {
		return errors.New("trusted-lan requires trusted source")
	}
	if !slices.Contains([]string{"auto", "review", "pinned", "manual"}, in.UpdatePolicy) || in.IntervalMinutes < 5 || in.IntervalMinutes > 43200 {
		return errors.New("invalid update policy or interval")
	}
	if in.URL != "" {
		if e := security.SafeURL(in.URL); e != nil {
			return e
		}
	}
	return security.ValidateHeaders(in.Headers)
}
func (s *Service) Preview(ctx context.Context, in Input) (model.Normalized, []byte, error) {
	if e := defaults(&in); e != nil {
		return model.Normalized{}, nil, e
	}
	if d, ok := Connectors[in.Protocol]; ok {
		if in.URL == "" {
			return model.Normalized{}, nil, errors.New("service URL required")
		}
		n := model.Normalized{Protocol: in.Protocol, MediaTypes: d.MediaTypes, Capabilities: d.Capabilities, Items: []model.Item{}, Warnings: []string{"Service credentials stay client-local unless a runtime gateway is configured"}}
		return n, jsonBytes(map[string]string{"protocol": in.Protocol, "url": in.URL}), nil
	}
	b := []byte(in.Content)
	if in.Content == "" {
		if in.URL == "" {
			return model.Normalized{}, nil, errors.New("provide URL or content")
		}
		res, e := s.Fetch.Get(ctx, in.URL, fetch.Policy{Network: in.Network, Trust: in.Trust}, in.Headers, 8<<20, false)
		if e != nil {
			return model.Normalized{}, nil, e
		}
		b = res.Body
	}
	n, e := adapter.Parse(b, in.Protocol, in.URL)
	return n, b, e
}
func (s *Service) saveSecret(ctx context.Context, tx *store.Tx, owner string, headers map[string]string) error {
	if headers == nil {
		return nil
	}
	if e := security.ValidateHeaders(headers); e != nil {
		return e
	}
	enc, e := s.Vault.Seal(jsonBytes(headers), owner)
	if e != nil {
		return e
	}
	v := model.Secret{ID: owner, OwnerID: owner, Ciphertext: enc, UpdatedAt: model.Now()}
	return store.Put(ctx, tx, "secrets", owner, v)
}
func (s *Service) Headers(ctx context.Context, owner string) (map[string]string, error) {
	v, e := store.Get[model.Secret](ctx, s.DB.Pool, "secrets", owner)
	if errors.Is(e, store.ErrNotFound) {
		return map[string]string{}, nil
	}
	if e != nil {
		return nil, e
	}
	b, e := s.Vault.Open(v.Ciphertext, owner)
	if e != nil {
		return nil, errors.New("credential decryption failed")
	}
	var h map[string]string
	e = json.Unmarshal(b, &h)
	return h, e
}
func (s *Service) Import(ctx context.Context, in Input) (model.Source, error) {
	if e := defaults(&in); e != nil {
		return model.Source{}, e
	}
	n, b, e := s.Preview(ctx, in)
	if e != nil {
		return model.Source{}, e
	}
	if n.Protocol == "catalog" {
		return model.Source{}, errors.New("add catalogs through the catalog subscription endpoint")
	}
	hash, e := s.Vault.Snapshot(b)
	if e != nil {
		return model.Source{}, e
	}
	src := newSource(in, n)
	rev := model.Revision{ID: model.ID("rev"), SourceID: src.ID, Hash: hash, Normalized: n, Status: "staged", Diff: adapter.Difference(model.Normalized{}, n), CreatedAt: model.Now()}
	src.StagedRevision = rev.ID
	e = s.DB.Write(ctx, func(tx *store.Tx) error {
		if e := s.validateRuntime(ctx, tx, src); e != nil {
			return e
		}
		if e := store.Insert(ctx, tx, "sources", src.ID, src); e != nil {
			return e
		}
		if e := store.Insert(ctx, tx, "revisions", rev.ID, rev); e != nil {
			return e
		}
		if e := s.saveSecret(ctx, tx, src.ID, in.Headers); e != nil {
			return e
		}
		if src.URL != "" {
			ep := model.Endpoint{ID: src.ID + "_primary", SourceID: src.ID, Role: "primary", URL: src.URL}
			if e := store.Put(ctx, tx, "endpoints", ep.ID, ep); e != nil {
				return e
			}
		}
		return audit(ctx, tx, "source.import", src.ID)
	})
	return src, e
}
func newSource(in Input, n model.Normalized) model.Source {
	mode := in.Mode
	if mode == "" {
		mode = "compiled"
		if _, ok := Connectors[n.Protocol]; ok {
			mode = "direct-client"
		}
		if n.RequiresRuntime && !strings.HasPrefix(n.Protocol, "legado-") {
			mode = "catalog-only"
		}
		if in.RuntimeID != "" {
			mode = "runtime-backed"
		}
	}
	media := n.MediaTypes
	if len(in.MediaTypes) > 0 {
		media = in.MediaTypes
	}
	return model.Source{ID: model.ID("src"), Name: in.Name, Protocol: n.Protocol, MediaTypes: media, Capabilities: n.Capabilities, Mode: mode, Trust: in.Trust, Network: in.Network, UpdatePolicy: in.UpdatePolicy, IntervalMinutes: in.IntervalMinutes, URL: in.URL, RuntimeID: in.RuntimeID, Health: "unknown", CreatedAt: model.Now(), UpdatedAt: model.Now()}
}

var mediaTypes = []string{"video.movie", "video.series", "video.short", "video.live", "text.novel", "text.ebook", "text.article", "image.comic", "audio.audiobook", "audio.podcast", "audio.music", "audio.radio", "speech.tts", "support.metadata", "support.subtitle", "support.danmaku", "support.epg", "support.lyric"}

func (s *Service) validateRuntime(ctx context.Context, q store.Reader, src model.Source) error {
	if !slices.Contains([]string{"catalog-only", "compiled", "runtime-backed", "direct-client"}, src.Mode) {
		return errors.New("unsupported execution mode")
	}
	if _, ok := Connectors[src.Protocol]; ok && src.Mode == "compiled" {
		return errors.New("media services use direct-client or runtime-backed mode")
	}
	if src.Mode == "direct-client" && src.URL == "" {
		return errors.New("direct-client source requires a URL")
	}
	for _, m := range src.MediaTypes {
		if !slices.Contains(mediaTypes, m) {
			return errors.New("unsupported media type")
		}
	}
	if src.RuntimeID != "" {
		rt, e := store.Get[model.Runtime](ctx, q, "runtimes", src.RuntimeID)
		if e != nil {
			return e
		}
		if src.Mode != "runtime-backed" {
			return errors.New("runtime mapping requires runtime-backed mode")
		}
		if !slices.Contains(Connectors[rt.Driver].Protocols, src.Protocol) {
			return errors.New("runtime does not support this source protocol")
		}
	} else if src.Mode == "runtime-backed" {
		return errors.New("runtime-backed source requires a runtime")
	}
	if b, e := adapter.Base(src.Protocol); e == nil && b.RequiresRuntime && src.Mode != "runtime-backed" && src.Mode != "catalog-only" && !(strings.HasPrefix(src.Protocol, "legado-") && src.Mode == "compiled") {
		return errors.New("executable rules require an isolated runtime")
	}
	return nil
}
func (s *Service) EditSource(ctx context.Context, id string, in Input) (model.Source, error) {
	var out model.Source
	if e := defaults(&in); e != nil {
		return out, e
	}
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		if in.Protocol != "" && in.Protocol != v.Protocol {
			return errors.New("protocol changes require importing a new source")
		}
		if in.URL != v.URL {
			return errors.New("endpoint changes require importing and reviewing a new source")
		}
		v.Name = in.Name
		v.Trust = in.Trust
		v.Network = in.Network
		v.UpdatePolicy = in.UpdatePolicy
		v.IntervalMinutes = in.IntervalMinutes
		v.RuntimeID = in.RuntimeID
		if in.Mode != "" {
			v.Mode = in.Mode
		}
		if len(in.MediaTypes) > 0 {
			v.MediaTypes = in.MediaTypes
		}
		if e = s.validateRuntime(ctx, tx, v); e != nil {
			return e
		}
		v.UpdatedAt = model.Now()
		if v.Health != "disabled" && v.Health != "quarantined" {
			v.Health = "unknown"
			v.Score = 0
		}
		if e = s.saveSecret(ctx, tx, id, in.Headers); e != nil {
			return e
		}
		if e = store.Put(ctx, tx, "sources", id, v); e != nil {
			return e
		}
		out = v
		return audit(ctx, tx, "source.edit", id)
	})
	return out, e
}
func (s *Service) SourceAction(ctx context.Context, id, action, revision string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		switch action {
		case "approve", "rollback":
			if revision == "" {
				revision = v.StagedRevision
			}
			r, e := store.Get[model.Revision](ctx, tx, "revisions", revision)
			if e != nil {
				return e
			}
			if r.SourceID != id {
				return errors.New("revision does not belong to source")
			}
			if r.Status == "invalid" {
				return errors.New("invalid revision cannot be approved")
			}
			if action == "rollback" && r.Status != "approved" {
				return errors.New("rollback requires a previously approved revision")
			}
			r.Status = "approved"
			if e = store.Put(ctx, tx, "revisions", r.ID, r); e != nil {
				return e
			}
			v.ActiveRevision = r.ID
			if r.ID == v.StagedRevision {
				v.StagedRevision = ""
			}
			if action == "rollback" {
				v.UpdatePolicy = "pinned"
			}
			if v.Health != "quarantined" && v.Health != "disabled" {
				v.Health = "degraded"
				v.Score = 60
			}
		case "reject":
			if v.StagedRevision == "" {
				return errors.New("no staged revision")
			}
			r, e := store.Get[model.Revision](ctx, tx, "revisions", v.StagedRevision)
			if e != nil {
				return e
			}
			r.Status = "rejected"
			if e = store.Put(ctx, tx, "revisions", r.ID, r); e != nil {
				return e
			}
			v.StagedRevision = ""
		case "enable":
			if v.ActiveRevision == "" {
				return errors.New("approve a revision before enabling")
			}
			v.Enabled = true
			if v.Health == "disabled" {
				v.Health = "unknown"
				v.Score = 0
			}
		case "disable":
			v.Enabled = false
			v.Health = "disabled"
		case "quarantine":
			v.Health = "quarantined"
			v.Score = 0
		case "release":
			if v.Health != "quarantined" {
				return errors.New("source is not quarantined")
			}
			v.Health = "unknown"
			v.Failures = 0
			v.Score = 0
		default:
			return errors.New("unknown source action")
		}
		v.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "sources", id, v); e != nil {
			return e
		}
		return audit(ctx, tx, "source."+action, id)
	})
}
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		sets, e := store.List[model.SourceSet](ctx, tx, "source_sets")
		if e != nil {
			return e
		}
		for _, set := range sets {
			for _, m := range set.Members {
				if m.SourceID == id {
					return errors.New("remove source from its source sets before deletion")
				}
			}
		}
		if e = store.Delete(ctx, tx, "sources", id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "DELETE FROM secrets WHERE id=$1", id); e != nil {
			return e
		}
		return audit(ctx, tx, "source.delete", id)
	})
}
func (s *Service) SyncSource(ctx context.Context, id string) error {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", id)
	if e != nil {
		return e
	}
	if src.UpdatePolicy == "pinned" {
		return errors.New("source is pinned")
	}
	if src.URL == "" {
		return errors.New("inline source has no remote URL")
	}
	if _, ok := Connectors[src.Protocol]; ok {
		return s.Probe(ctx, id)
	}
	h, e := s.Headers(ctx, id)
	if e != nil {
		return e
	}
	if src.ETag != "" {
		h["If-None-Match"] = src.ETag
	}
	if src.LastModified != "" {
		h["If-Modified-Since"] = src.LastModified
	}
	res, e := s.Fetch.Get(ctx, src.URL, fetch.Policy{Network: src.Network, Trust: src.Trust}, h, 8<<20, false)
	if e != nil {
		return s.recordFailure(ctx, src, "fetch_failed", e)
	}
	var n model.Normalized
	var sample *model.Probe
	hash := ""
	if res.Status != 304 {
		hash, e = s.Vault.Snapshot(res.Body)
		if e != nil {
			return e
		}
		n, e = adapter.Parse(res.Body, src.Protocol, src.URL)
		if e != nil {
			invalid := model.Revision{ID: model.ID("rev"), SourceID: src.ID, Hash: hash, Normalized: model.Normalized{Protocol: src.Protocol, Items: []model.Item{}, Warnings: []string{"Structure validation failed"}}, Status: "invalid", CreatedAt: model.Now()}
			if saveErr := s.DB.Write(ctx, func(tx *store.Tx) error { return store.Insert(ctx, tx, "revisions", invalid.ID, invalid) }); saveErr != nil {
				return saveErr
			}
			return s.recordFailure(ctx, src, "structure_invalid", e)
		}
		if src.UpdatePolicy == "auto" {
			p, _ := s.sample(ctx, src, n)
			sample = &p
		}
	}
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		if v.UpdatedAt != src.UpdatedAt {
			return ErrConflict
		}
		v.LastChecked = model.Now()
		v.NextSync = time.Now().Add(time.Duration(v.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
		if res.Status != 304 || res.ETag != "" {
			v.ETag = res.ETag
		}
		if res.Status != 304 || res.LastModified != "" {
			v.LastModified = res.LastModified
		}
		if sample != nil {
			if e = store.Insert(ctx, tx, "probes", sample.ID, *sample); e != nil {
				return e
			}
		}
		if hash != "" {
			old := model.Revision{}
			if v.ActiveRevision != "" {
				old, e = store.Get[model.Revision](ctx, tx, "revisions", v.ActiveRevision)
				if e != nil {
					return e
				}
			}
			same := old.Hash == hash
			revisions, err := store.List[model.Revision](ctx, tx, "revisions")
			if err != nil {
				return err
			}
			for _, previous := range revisions {
				if previous.SourceID == id && previous.Hash == hash && previous.Status == "rejected" {
					same = true
				}
			}
			if v.StagedRevision != "" {
				pending, e := store.Get[model.Revision](ctx, tx, "revisions", v.StagedRevision)
				if e != nil {
					return e
				}
				same = same || pending.Hash == hash
			}
			if !same {
				diff := adapter.Difference(old.Normalized, n)
				r := model.Revision{ID: model.ID("rev"), SourceID: id, Hash: hash, Normalized: n, Status: "staged", Diff: diff, CreatedAt: model.Now()}
				if v.UpdatePolicy == "auto" && !diff.RequiresReview && v.ActiveRevision != "" && v.Health != "quarantined" && sample != nil && sample.Success && sample.Level == "functional" {
					r.Status = "approved"
					v.ActiveRevision = r.ID
					v.StagedRevision = ""
				} else {
					v.StagedRevision = r.ID
				}
				if e = store.Insert(ctx, tx, "revisions", r.ID, r); e != nil {
					return e
				}
			}
		}
		v.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "sources", id, v); e != nil {
			return e
		}
		return audit(ctx, tx, "source.sync", id)
	})
}
func (s *Service) recordFailure(ctx context.Context, src model.Source, code string, cause error) error {
	e := s.saveProbe(ctx, src, model.Probe{ID: model.ID("probe"), SourceID: src.ID, Level: "structure", Code: code, Checks: []string{code}, CreatedAt: model.Now()})
	if e != nil {
		return e
	}
	return cause
}
func (s *Service) saveProbe(ctx context.Context, src model.Source, p model.Probe) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Source](ctx, tx, "sources", src.ID)
		if e != nil {
			return e
		}
		if v.UpdatedAt != src.UpdatedAt || v.ActiveRevision != src.ActiveRevision || v.RuntimeID != src.RuntimeID {
			return ErrConflict
		}
		v.LastChecked = model.Now()
		if p.Success {
			v.Failures = 0
			if v.Health != "quarantined" && v.Health != "disabled" {
				v.Health = "healthy"
				v.Score = 100
				if p.Level != "functional" {
					v.Health = "degraded"
					v.Score = 60
				}
			}
		} else {
			v.Failures++
			if v.Health != "disabled" {
				v.Score = max(0, 60-v.Failures*20)
				v.Health = "degraded"
				if v.Failures >= 2 {
					v.Health = "failing"
				}
				if v.Failures >= 3 {
					v.Health = "quarantined"
				}
			}
		}
		v.UpdatedAt = model.Now()
		if e = store.Insert(ctx, tx, "probes", p.ID, p); e != nil {
			return e
		}
		return store.Put(ctx, tx, "sources", v.ID, v)
	})
}
func (s *Service) Probe(ctx context.Context, id string) error {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", id)
	if e != nil {
		return e
	}
	revID := src.ActiveRevision
	if revID == "" {
		revID = src.StagedRevision
	}
	r, e := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", revID)
	if e != nil {
		return e
	}
	p, probeErr := s.sample(ctx, src, r.Normalized)
	if e := s.saveProbe(ctx, src, p); e != nil {
		return e
	}
	return probeErr
}
func (s *Service) sample(ctx context.Context, src model.Source, n model.Normalized) (model.Probe, error) {
	start := time.Now()
	p := model.Probe{ID: model.ID("probe"), SourceID: src.ID, Level: "structure", Success: true, Code: "structure_valid", Checks: []string{"normalized_configuration_valid"}, CreatedAt: model.Now()}
	h, e := s.Headers(ctx, src.ID)
	if e != nil {
		p.Success = false
		p.Code = "credential_unavailable"
		return p, e
	}
	policy := fetch.Policy{Network: src.Network, Trust: src.Trust}
	if src.RuntimeID != "" {
		e = s.TestRuntime(ctx, src.RuntimeID, false)
		p.Level = "service"
		p.Checks = append(p.Checks, "runtime_service_api")
	} else if c, ok := Connectors[src.Protocol]; ok {
		res, err := s.Fetch.Get(ctx, strings.TrimRight(src.URL, "/")+c.StatusPath, policy, h, 2<<20, false)
		e = err
		if e == nil {
			_, e = validateRuntimeResponse(c, res.Body)
		}
		p.Level = "service"
		p.Checks = append(p.Checks, "service_api_structure")
	} else {
		switch src.Protocol {
		case "m3u":
			if len(n.Items) > 0 {
				res, err := s.Fetch.Get(ctx, n.Items[0].URL, policy, nil, 4096, true)
				e = err
				if e == nil {
					body := strings.TrimSpace(string(res.Body))
					if strings.HasPrefix(body, "#EXTM3U") {
						var segment string
						for _, line := range strings.Split(body, "\n") {
							line = strings.TrimSpace(line)
							if line != "" && !strings.HasPrefix(line, "#") {
								segment = line
								break
							}
						}
						if segment == "" {
							e = errors.New("HLS sample has no segment")
						} else {
							segment = resolveURL(n.Items[0].URL, segment)
							_, e = s.Fetch.Get(ctx, segment, policy, nil, 4096, true)
						}
					} else if strings.Contains(strings.ToLower(res.ContentType), "text/html") || strings.HasPrefix(body, "<html") {
						e = errors.New("stream sample is HTML")
					}
				}
				p.Level = "functional"
				p.Checks = append(p.Checks, "first_channel_media_sample")
			}
		case "tvbox":
			for _, item := range n.Items {
				if item.Group == "site" {
					res, err := s.Fetch.Get(ctx, item.URL, policy, nil, 1<<20, false)
					e = err
					if e == nil {
						var v map[string]any
						if json.Unmarshal(res.Body, &v) != nil || (v["class"] == nil && v["list"] == nil) {
							e = errors.New("CMS response has no class or list")
						}
					}
					p.Level = "functional"
					p.Checks = append(p.Checks, "first_http_cms_catalog")
					break
				}
			}
		case "rss", "atom", "json-feed", "opds1", "opds2":
			for _, item := range n.Items {
				if item.URL != "" {
					_, e = s.Fetch.Get(ctx, item.URL, policy, nil, 4096, true)
					p.Level = "functional"
					p.Checks = append(p.Checks, "first_item_reachable")
					break
				}
			}
		}
	}
	if e != nil {
		p.Success = false
		p.Code = "functional_probe_failed"
	} else if p.Level == "functional" {
		p.Code = "sample_passed"
	}
	p.LatencyMS = time.Since(start).Milliseconds()
	return p, e
}
func requireName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 160 {
		return fmt.Errorf("name must contain 1–160 characters")
	}
	return nil
}
func (s *Service) SetHeaders(ctx context.Context, id string, h map[string]string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		src, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e == nil {
			src.UpdatedAt = model.Now()
			if src.Health != "disabled" && src.Health != "quarantined" {
				src.Health = "unknown"
				src.Score = 0
			}
			if e = store.Put(ctx, tx, "sources", id, src); e != nil {
				return e
			}
		} else if errors.Is(e, store.ErrNotFound) {
			rt, e := store.Get[model.Runtime](ctx, tx, "runtimes", id)
			if e != nil {
				return e
			}
			rt.UpdatedAt = model.Now()
			rt.Health = "unknown"
			if e = store.Put(ctx, tx, "runtimes", id, rt); e != nil {
				return e
			}
		} else {
			return e
		}
		if e = s.saveSecret(ctx, tx, id, h); e != nil {
			return e
		}
		return audit(ctx, tx, "secret.replace", id)
	})
}
