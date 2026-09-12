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
	Type string // tvbox | iptv | novel | manga | audiobook | music | rss | other
	Slug string // stable SourceSet id, e.g. aggregate-tvbox
	Name string
}

var aggregateBuckets = []AggregateBucket{
	{Type: "tvbox", Slug: "aggregate-tvbox", Name: "聚合·TVBox"},
	{Type: "iptv", Slug: "aggregate-iptv", Name: "聚合·IPTV"},
	{Type: "novel", Slug: "aggregate-novel", Name: "聚合·小说"},
	{Type: "manga", Slug: "aggregate-manga", Name: "聚合·漫画"},
	{Type: "audiobook", Slug: "aggregate-audiobook", Name: "聚合·有声书"},
	{Type: "music", Slug: "aggregate-music", Name: "聚合·音乐"},
	{Type: "rss", Slug: "aggregate-rss", Name: "聚合·RSS"},
	{Type: "other", Slug: "aggregate-other", Name: "聚合·其他"},
}

// legacyAggregateSlugs are retired bucket IDs kept only so membership cleanup/reconcile can clear them.
var legacyAggregateSlugs = []string{"aggregate-legado"}

func AggregateDefs() []AggregateBucket { return slices.Clone(aggregateBuckets) }

func IsAggregateSetID(id string) bool {
	for _, b := range aggregateBuckets {
		if b.Slug == id {
			return true
		}
	}
	return slices.Contains(legacyAggregateSlugs, id)
}

// AggregateTypeFor maps a source protocol (and mediaTypes when needed) to a bucket type.
func AggregateTypeFor(protocol string, mediaTypes []string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	// Media-tag override for feeds that stay on rss/atom/json-feed/opml protocols.
	if slices.Contains(mediaTypes, "audio.music") && (p == "rss" || p == "atom" || p == "json-feed" || p == "opml" || p == "") {
		return "music"
	}
	switch p {
	case "tvbox":
		return "tvbox"
	case "m3u", "xmltv", "dispatcharr":
		return "iptv"
	case "legado-book", "so-novel", "relay-book", "opds1", "opds2", "legado-replace", "legado-hub":
		return "novel"
	case "mihon-repo":
		return "manga"
	case "legado-tts", "podcast":
		return "audiobook"
	case "lx-music", "music-playlist":
		return "music"
	case "direct-link":
		return "other"
	case "rss", "atom", "json-feed", "opml", "legado-rss":
		return "rss"
	}
	for _, m := range mediaTypes {
		switch m {
		case "video.live", "support.epg", "audio.radio":
			return "iptv"
		case "text.novel", "text.ebook":
			return "novel"
		case "image.comic":
			return "manga"
		case "speech.tts", "audio.audiobook":
			return "audiobook"
		case "audio.music":
			return "music"
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

func aggregateBindingID(setID string) string  { return "binding_" + setID }
func aggregateTokenOwner(setID string) string { return "aggregate_token_" + setID }

// AggregateInfo is the operator-facing summary for GET /api/v1/aggregates.
type AggregateInfo struct {
	Type              string            `json:"type"`
	Slug              string            `json:"slug"`
	SetID             string            `json:"setId"`
	Name              string            `json:"name"`
	MemberCount       int               `json:"memberCount"`
	PublicationID     string            `json:"publicationId,omitempty"`
	BindingID         string            `json:"bindingId,omitempty"`
	Token             string            `json:"token,omitempty"`
	SubscribeURLs     []string          `json:"subscribeUrls"`
	SubscribeByClient map[string]string `json:"subscribeByClient,omitempty"`
}

// defaultFormatsFor returns formats the publisher typically emits for a bucket
// when the current publication is unknown (no artifacts discovered yet).
func defaultFormatsFor(bucketType string) map[string]bool {
	m := map[string]bool{"shadow.json": true}
	switch bucketType {
	case "novel":
		m["legado/books.json"] = true
		m["hub/plugins.json"] = true
	case "tvbox":
		m["tvbox/store.json"] = true
	case "iptv":
		m["iptv/live.m3u"] = true
	case "music":
		m["lx-music/sources.json"] = true
		m["music/playlist.m3u"] = true
	case "audiobook":
		m["legado/tts.json"] = true
	case "manga":
		m["mihon/repos.json"] = true
	case "rss":
		m["legado/rss.json"] = true
		m["feeds.opml"] = true
	}
	return m
}

// aggregateSubscribe builds client-aware subscribe URLs for an aggregate bucket.
// Flattened subscribeUrls put the primary client format first. Only formats present
// in available (current publication artifacts) are advertised; if none are known,
// assume the formats that publisher always emits for that type.
func aggregateSubscribe(base, token, bucketType string, available map[string]bool) ([]string, map[string]string) {
	if token == "" {
		return nil, nil
	}
	if len(available) == 0 {
		available = defaultFormatsFor(bucketType)
	}
	prefix := "/p/" + token + "/"
	if base != "" {
		prefix = strings.TrimRight(base, "/") + prefix
	}
	byClient := map[string]string{}
	var urls []string
	add := func(client, path string) {
		if !available[path] {
			return
		}
		u := prefix + path
		if _, ok := byClient[client]; !ok {
			byClient[client] = u
		}
		for _, existing := range urls {
			if existing == u {
				return
			}
		}
		urls = append(urls, u)
	}
	switch bucketType {
	case "novel":
		add("legado", "legado/books.json")
		add("hub", "hub/plugins.json")
		add("shadowMedia", "shadow.json")
	case "tvbox":
		add("tvbox", "tvbox/store.json")
		add("shadowMedia", "shadow.json")
	case "iptv":
		add("iptv", "iptv/live.m3u")
		add("shadowMedia", "shadow.json")
	case "music":
		add("lxMusic", "lx-music/sources.json")
		add("playlist", "music/playlist.m3u")
		add("shadowMedia", "shadow.json")
	case "audiobook":
		add("legado", "legado/tts.json")
		add("shadowMedia", "shadow.json")
	case "manga":
		add("mihon", "mihon/repos.json")
		add("shadowMedia", "shadow.json")
	case "rss":
		add("legado", "legado/rss.json")
		add("opml", "feeds.opml")
		add("shadowMedia", "shadow.json")
	default:
		add("shadowMedia", "shadow.json")
	}
	if len(urls) == 0 && available["shadow.json"] {
		add("shadowMedia", "shadow.json")
	}
	return urls, byClient
}

func (s *Service) publicationFormats(ctx context.Context, publicationID string) map[string]bool {
	out := map[string]bool{}
	if publicationID == "" {
		return out
	}
	pub, e := store.Get[model.Publication](ctx, s.DB.Pool, "publications", publicationID)
	if e != nil {
		return out
	}
	for path := range pub.Artifacts {
		out[path] = true
	}
	return out
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
		if b, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", bID); e == nil && !b.Revoked {
			info.BindingID = bID
			if tok, err := s.aggregateToken(ctx, def.Slug); err == nil && tok != "" {
				info.Token = tok
				available := s.publicationFormats(ctx, info.PublicationID)
				info.SubscribeURLs, info.SubscribeByClient = aggregateSubscribe(base, tok, def.Type, available)
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
			return s.ensureAggregatePublished(ctx, bucketByType(AggregateTypeFor(src.Protocol, src.MediaTypes)).Type)
		}
		return nil
	default:
		return nil
	}
}

func (s *Service) upsertAggregateMember(ctx context.Context, src model.Source) error {
	def := bucketByType(AggregateTypeFor(src.Protocol, src.MediaTypes))
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		if e := stripAggregateMembershipTx(ctx, tx, src.ID); e != nil {
			return e
		}
		set, e := s.ensureAggregateSetTx(ctx, tx, def)
		if e != nil {
			return e
		}
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
		// Intentional empty: withdraw stale publication so clients stop serving removed members.
		return s.withdrawAggregatePublication(ctx, def.Slug)
	}
	_, e = s.Publish(ctx, def.Slug)
	if e != nil {
		var pe *PublicationError
		if errors.As(e, &pe) {
			// Members remain but none eligible (health/update failure): keep last good.
			return nil
		}
		// Transient publish failure: enqueue durable retry (coalesced) without blocking callers forever.
		if _, enq := s.Enqueue(ctx, "set.publish", def.Slug); enq != nil && !errors.Is(enq, store.ErrNotFound) {
			return errors.Join(e, enq)
		}
		return e
	}
	return nil
}

// withdrawAggregatePublication publishes an empty Bundle and moves the stable pointer.
func (s *Service) withdrawAggregatePublication(ctx context.Context, setID string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		set, e := store.Get[model.SourceSet](ctx, tx, "source_sets", setID)
		if e != nil {
			return e
		}
		if len(set.Members) != 0 {
			return nil
		}
		if set.PublishSignature == "empty-aggregate" && set.CurrentPublication != "" {
			return nil
		}
		created := model.Now()
		pub := model.Publication{
			ID: model.ID("pub"), SetID: setID, SourceRevisions: map[string]string{}, Exclusions: map[string]string{},
			Artifacts: map[string]model.Artifact{}, FormatWarnings: map[string]string{"shadow.json": "aggregate has no enabled members"},
			CreatedAt: created,
		}
		bundle := map[string]any{
			"schema": "shadow.media.bundle/v1", "bundleId": setID, "name": set.Name, "publicationId": pub.ID,
			"revision": "", "generatedAt": created, "providers": []any{}, "exports": map[string]string{},
			"formatWarnings": pub.FormatWarnings,
		}
		body := jsonBytes(bundle)
		pub.Revision = "sha256:" + security.Hash(body)
		bundle["revision"] = pub.Revision
		body = jsonBytes(bundle)
		pub.Artifacts["shadow.json"] = model.Artifact{ContentType: "application/json", Body: string(body), Hash: security.Hash(body)}
		if e = store.Insert(ctx, tx, "publications", pub.ID, pub); e != nil {
			return e
		}
		set.PreviousPublication = set.CurrentPublication
		set.CurrentPublication = pub.ID
		set.PublishSignature = "empty-aggregate"
		set.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
			return e
		}
		return audit(ctx, tx, "aggregate.withdraw", setID)
	})
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

// ReconcileResult summarizes POST /api/v1/aggregates/reconcile.
type ReconcileResult struct {
	Buckets        []AggregateInfo `json:"buckets"`
	EnabledSources int             `json:"enabledSources"`
	Memberships    int             `json:"memberships"`
}

// ReconcileAggregates rebuilds aggregate memberships from all enabled, approved sources.
// Use after bucket renames (e.g. aggregate-legado → novel/manga/audiobook/music).
func (s *Service) ReconcileAggregates(ctx context.Context, publicBase string) (ReconcileResult, error) {
	sources, e := store.List[model.Source](ctx, s.DB.Pool, "sources")
	if e != nil {
		return ReconcileResult{}, e
	}
	eligible := make([]model.Source, 0, len(sources))
	for _, src := range sources {
		if src.Enabled && src.ActiveRevision != "" {
			eligible = append(eligible, src)
		}
	}
	e = s.DB.Write(ctx, func(tx *store.Tx) error {
		sets, e := store.List[model.SourceSet](ctx, tx, "source_sets")
		if e != nil {
			return e
		}
		for _, set := range sets {
			if !IsAggregateSetID(set.ID) {
				continue
			}
			if len(set.Members) == 0 {
				continue
			}
			set.Members = nil
			set.UpdatedAt = model.Now()
			set.PublishSignature = ""
			if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
				return e
			}
		}
		return audit(ctx, tx, "aggregate.reconcile.clear", "")
	})
	if e != nil {
		return ReconcileResult{}, e
	}
	for _, src := range eligible {
		if e := s.upsertAggregateMember(ctx, src); e != nil {
			return ReconcileResult{}, e
		}
	}
	for _, def := range aggregateBuckets {
		if e := s.ensureAggregatePublished(ctx, def.Type); e != nil {
			return ReconcileResult{}, e
		}
	}
	infos, e := s.ListAggregates(ctx, publicBase)
	if e != nil {
		return ReconcileResult{}, e
	}
	members := 0
	for _, info := range infos {
		members += info.MemberCount
	}
	return ReconcileResult{Buckets: infos, EnabledSources: len(eligible), Memberships: members}, nil
}
