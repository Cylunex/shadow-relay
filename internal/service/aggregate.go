package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// Aggregate buckets group client-visible sources by protocol/media so operators
// and clients subscribe to one type set instead of managing SourceSets manually.
type AggregateBucket struct {
	Type string // tvbox | iptv | legado | rss | other
	Slug string // stable SourceSet id, e.g. aggregate-tvbox
	Name string
}

var aggregateBuckets = []AggregateBucket{
	{Type: "tvbox", Slug: "aggregate-tvbox", Name: "聚合·TVBox"},
	{Type: "iptv", Slug: "aggregate-iptv", Name: "聚合·IPTV"},
	{Type: "legado", Slug: "aggregate-legado", Name: "聚合·阅读"},
	{Type: "rss", Slug: "aggregate-rss", Name: "聚合·RSS"},
	{Type: "other", Slug: "aggregate-other", Name: "聚合·其他"},
}

func AggregateDefs() []AggregateBucket { return slices.Clone(aggregateBuckets) }

func IsAggregateSetID(id string) bool {
	for _, b := range aggregateBuckets {
		if b.Slug == id {
			return true
		}
	}
	return false
}

// AggregateTypeFor maps a source protocol (and mediaTypes when needed) to a bucket type.
func AggregateTypeFor(protocol string, mediaTypes []string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	switch p {
	case "tvbox":
		return "tvbox"
	case "m3u", "xmltv", "dispatcharr":
		return "iptv"
	case "legado-book", "legado-rss", "legado-tts", "legado-replace", "legado-hub",
		"so-novel", "relay-book", "mihon-repo", "opds1", "opds2":
		return "legado"
	case "rss", "atom", "json-feed", "opml", "podcast":
		return "rss"
	}
	for _, m := range mediaTypes {
		switch m {
		case "video.live", "support.epg", "audio.radio":
			return "iptv"
		case "text.novel", "text.ebook", "image.comic", "speech.tts":
			return "legado"
		case "text.article", "audio.podcast":
			return "rss"
		case "video.movie", "video.series", "video.short":
			if p == "" {
				return "tvbox"
			}
		}
	}
	return "other"
}

func bucketByType(t string) AggregateBucket {
	for _, b := range aggregateBuckets {
		if b.Type == t {
			return b
		}
	}
	return aggregateBuckets[len(aggregateBuckets)-1]
}

func aggregateBindingID(setID string) string { return "binding_" + setID }
func aggregateTokenOwner(setID string) string { return "aggregate_token_" + setID }

// AggregateInfo is the operator-facing summary for GET /api/v1/aggregates.
type AggregateInfo struct {
	Type          string   `json:"type"`
	Slug          string   `json:"slug"`
	SetID         string   `json:"setId"`
	Name          string   `json:"name"`
	MemberCount   int      `json:"memberCount"`
	PublicationID string   `json:"publicationId,omitempty"`
	BindingID     string   `json:"bindingId,omitempty"`
	Token         string   `json:"token,omitempty"`
	SubscribeURLs []string `json:"subscribeUrls"`
}

func (s *Service) ListAggregates(ctx context.Context, publicBase string) ([]AggregateInfo, error) {
	base := strings.TrimRight(publicBase, "/")
	out := make([]AggregateInfo, 0, len(aggregateBuckets))
	for _, def := range aggregateBuckets {
		info := AggregateInfo{Type: def.Type, Slug: def.Slug, SetID: def.Slug, Name: def.Name, SubscribeURLs: []string{}}
		set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", def.Slug)
		if e == nil {
			info.MemberCount = len(set.Members)
			info.PublicationID = set.CurrentPublication
		} else if !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		bID := aggregateBindingID(def.Slug)
		if _, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", bID); e == nil {
			info.BindingID = bID
			if tok, err := s.aggregateToken(ctx, def.Slug); err == nil && tok != "" {
				info.Token = tok
				path := "/p/" + tok + "/shadow.json"
				if base != "" {
					info.SubscribeURLs = []string{base + path}
				} else {
					info.SubscribeURLs = []string{path}
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) aggregateToken(ctx context.Context, setID string) (string, error) {
	owner := aggregateTokenOwner(setID)
	sec, e := store.Get[model.Secret](ctx, s.DB.Pool, "secrets", owner)
	if e != nil {
		return "", e
	}
	b, e := s.Vault.Open(sec.Ciphertext, owner)
	if e != nil {
		return "", e
	}
	var payload struct {
		Token string `json:"token"`
	}
	if e = json.Unmarshal(b, &payload); e != nil {
		return "", e
	}
	return payload.Token, nil
}

// syncAggregateAfterVisibility runs after SourceAction enable/disable commits.
// Client-visible sources require an approved revision AND enabled=true (see publish eligibility).
func (s *Service) syncAggregateAfterVisibility(ctx context.Context, src model.Source, action string) error {
	switch action {
	case "enable":
		if e := s.upsertAggregateMember(ctx, src); e != nil {
			return e
		}
		return s.ensureAggregatePublished(ctx, AggregateTypeFor(src.Protocol, src.MediaTypes))
	case "disable":
		setID, e := s.removeFromAggregates(ctx, src.ID)
		if e != nil {
			return e
		}
		if setID != "" {
			_ = s.ensureAggregatePublished(ctx, bucketByType(AggregateTypeFor(src.Protocol, src.MediaTypes)).Type)
		}
		return nil
	default:
		return nil
	}
}

func (s *Service) upsertAggregateMember(ctx context.Context, src model.Source) error {
	def := bucketByType(AggregateTypeFor(src.Protocol, src.MediaTypes))
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		set, e := s.ensureAggregateSetTx(ctx, tx, def)
		if e != nil {
			return e
		}
		found := false
		for _, m := range set.Members {
			if m.SourceID == src.ID {
				found = true
				break
			}
		}
		if !found {
			set.Members = append(set.Members, model.Member{
				SourceID: src.ID, Priority: 100, Weight: 1, Role: "primary",
				MinScore: 0, TimeoutMS: 15000, MaxConcurrency: 2,
			})
			set.UpdatedAt = model.Now()
			set.PublishSignature = ""
			if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
				return e
			}
			if e = audit(ctx, tx, "aggregate.member.add", src.ID); e != nil {
				return e
			}
		}
		return s.ensureAggregateBindingTx(ctx, tx, def.Slug)
	})
}

func (s *Service) removeFromAggregates(ctx context.Context, sourceID string) (string, error) {
	var touched string
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		sets, e := store.List[model.SourceSet](ctx, tx, "source_sets")
		if e != nil {
			return e
		}
		for _, set := range sets {
			if !IsAggregateSetID(set.ID) {
				continue
			}
			next := set.Members[:0]
			removed := false
			for _, m := range set.Members {
				if m.SourceID == sourceID {
					removed = true
					continue
				}
				next = append(next, m)
			}
			if !removed {
				continue
			}
			set.Members = next
			set.UpdatedAt = model.Now()
			set.PublishSignature = ""
			if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
				return e
			}
			touched = set.ID
			if e = audit(ctx, tx, "aggregate.member.remove", sourceID); e != nil {
				return e
			}
		}
		return nil
	})
	return touched, e
}

func (s *Service) ensureAggregateSetTx(ctx context.Context, tx *store.Tx, def AggregateBucket) (model.SourceSet, error) {
	set, e := store.Get[model.SourceSet](ctx, tx, "source_sets", def.Slug)
	if e == nil {
		return set, nil
	}
	if !errors.Is(e, store.ErrNotFound) {
		return model.SourceSet{}, e
	}
	set = model.SourceSet{
		ID:                 def.Slug,
		Name:               def.Name,
		Description:        "System aggregate by content type (" + def.Type + "). Clients subscribe here; do not use for manual SourceSets.",
		Members:            []model.Member{},
		AutoPublish:        true,
		MinAvailable:       1,
		MaxExcludedPercent: 100,
		UpdatedAt:          model.Now(),
	}
	if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
		return model.SourceSet{}, e
	}
	return set, audit(ctx, tx, "aggregate.set.ensure", set.ID)
}

func (s *Service) ensureAggregateBindingTx(ctx context.Context, tx *store.Tx, setID string) error {
	bID := aggregateBindingID(setID)
	if _, e := store.Get[model.Binding](ctx, tx, "bindings", bID); e == nil {
		return nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return e
	}
	token := security.Token()
	b := model.Binding{
		ID:         bID,
		Name:       "system:" + setID,
		SetID:      setID,
		Hash:       security.Hash([]byte(token)),
		Formats:    slices.Clone(Formats),
		ExpiresAt:  time.Now().AddDate(100, 0, 0).UTC().Format(time.RFC3339),
		Generation: 1,
		CreatedAt:  model.Now(),
	}
	if e := store.Insert(ctx, tx, "bindings", b.ID, b); e != nil {
		return e
	}
	owner := aggregateTokenOwner(setID)
	enc, e := s.Vault.Seal(jsonBytes(map[string]string{"token": token}), owner)
	if e != nil {
		return e
	}
	sec := model.Secret{ID: owner, OwnerID: owner, Ciphertext: enc, UpdatedAt: model.Now()}
	if e = store.Put(ctx, tx, "secrets", owner, sec); e != nil {
		return e
	}
	return audit(ctx, tx, "aggregate.binding.ensure", bID)
}

func (s *Service) ensureAggregatePublished(ctx context.Context, typeName string) error {
	def := bucketByType(typeName)
	set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", def.Slug)
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			return nil
		}
		return e
	}
	if len(set.Members) == 0 {
		return nil
	}
	_, e = s.Publish(ctx, def.Slug)
	if e != nil {
		// Empty eligibility after disable is expected; keep prior publication.
		if strings.Contains(e.Error(), "no eligible sources") {
			return nil
		}
		return e
	}
	return nil
}

// stripAggregateMembershipTx removes sourceID from aggregate sets only (manual sets untouched).
func stripAggregateMembershipTx(ctx context.Context, tx *store.Tx, sourceID string) error {
	sets, e := store.List[model.SourceSet](ctx, tx, "source_sets")
	if e != nil {
		return e
	}
	for _, set := range sets {
		if !IsAggregateSetID(set.ID) {
			continue
		}
		next := []model.Member{}
		changed := false
		for _, m := range set.Members {
			if m.SourceID == sourceID {
				changed = true
				continue
			}
			next = append(next, m)
		}
		if !changed {
			continue
		}
		set.Members = next
		set.UpdatedAt = model.Now()
		set.PublishSignature = ""
		if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
			return e
		}
	}
	return nil
}
