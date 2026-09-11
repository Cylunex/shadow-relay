package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func TestImportAutoEnablesReviewedAndJoinsAggregate(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src, err := s.Import(ctx, Input{Name: "Live IPTV", Content: "#EXTM3U\n#EXTINF:-1,Demo\nhttps://media.example.com/demo.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if !src.Enabled || src.ActiveRevision == "" || src.StagedRevision != "" {
		t.Fatalf("expected live source after reviewed import: %+v", src)
	}
	set, err := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil || len(set.Members) != 1 || set.Members[0].SourceID != src.ID {
		t.Fatalf("expected aggregate membership: %v %+v", err, set)
	}
	if set.CurrentPublication == "" {
		t.Fatal("aggregate should publish after auto-enable")
	}
}

func TestImportUntrustedStaysStaged(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src, err := s.Import(ctx, Input{Name: "Risky", Content: playlist, Trust: "untrusted"})
	if err != nil {
		t.Fatal(err)
	}
	if src.Enabled || src.ActiveRevision != "" || src.StagedRevision == "" {
		t.Fatalf("untrusted must stay staged: %+v", src)
	}
}

func TestEmptyAggregateWithdrawsStalePublication(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, "#EXTM3U\n#EXTINF:-1,Demo\nhttps://media.example.com/demo.ts")
	set, err := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil || set.CurrentPublication == "" {
		t.Fatalf("need published aggregate: %v %+v", err, set)
	}
	oldPub := set.CurrentPublication
	tok, err := s.aggregateToken(ctx, "aggregate-iptv")
	if err != nil || tok == "" {
		t.Fatal(err)
	}
	art, err := s.Resolve(ctx, tok, "", "shadow.json")
	if err != nil || !strings.Contains(art.Body, src.ID) {
		t.Fatalf("expected member in publication: %v %s", err, art.Body)
	}
	if err := s.SourceAction(ctx, src.ID, "disable", ""); err != nil {
		t.Fatal(err)
	}
	set, err = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Members) != 0 {
		t.Fatalf("members remain: %+v", set.Members)
	}
	if set.CurrentPublication == "" || set.CurrentPublication == oldPub {
		t.Fatalf("expected withdrawn empty publication, got %q (was %q)", set.CurrentPublication, oldPub)
	}
	if set.PublishSignature != "empty-aggregate" {
		t.Fatalf("signature: %q", set.PublishSignature)
	}
	art, err = s.Resolve(ctx, tok, "", "shadow.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]any
	if err := json.Unmarshal([]byte(art.Body), &bundle); err != nil {
		t.Fatal(err)
	}
	providers, _ := bundle["providers"].([]any)
	if len(providers) != 0 {
		t.Fatalf("empty aggregate still serves providers: %#v", providers)
	}
	if strings.Contains(art.Body, src.ID) {
		t.Fatal("stale member still in empty publication")
	}
}

func TestHealthFailureKeepsLastGoodAggregatePublication(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	a := imported(t, s, "#EXTM3U\n#EXTINF:-1,A\nhttps://media.example.com/a.ts")
	b := imported(t, s, "#EXTM3U\n#EXTINF:-1,B\nhttps://media.example.com/b.ts")
	set, err := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil || set.CurrentPublication == "" {
		t.Fatal(err)
	}
	good := set.CurrentPublication
	// Force both members into avoid without removing membership.
	for _, id := range []string{a.ID, b.ID} {
		_ = s.DB.Write(ctx, func(tx *store.Tx) error {
			src, e := store.Get[model.Source](ctx, tx, "sources", id)
			if e != nil {
				return e
			}
			src.Health = "avoid"
			src.Score = 0
			src.Failures = 3
			return store.Put(ctx, tx, "sources", id, src)
		})
	}
	if err := s.ensureAggregatePublished(ctx, "iptv"); err != nil {
		t.Fatal(err)
	}
	set, err = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil {
		t.Fatal(err)
	}
	if set.CurrentPublication != good {
		t.Fatalf("health failure replaced last good: %s -> %s", good, set.CurrentPublication)
	}
}

func TestAggregateRotateUpdatesDisplayedToken(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	_ = imported(t, s, "#EXTM3U\n#EXTINF:-1,Demo\nhttps://media.example.com/demo.ts")
	infos, err := s.ListAggregates(ctx, "https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var before string
	var bindingID string
	for _, info := range infos {
		if info.Slug == "aggregate-iptv" {
			before, bindingID = info.Token, info.BindingID
		}
	}
	if before == "" || bindingID == "" {
		t.Fatal("missing aggregate token")
	}
	next, err := s.BindingAction(ctx, bindingID, "rotate")
	if err != nil || next == "" || next == before {
		t.Fatalf("rotate failed: %v %q", err, next)
	}
	infos, err = s.ListAggregates(ctx, "https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Slug == "aggregate-iptv" {
			if info.Token != next {
				t.Fatalf("settings still show stale token: got %q want %q", info.Token, next)
			}
		}
	}
	if _, err := s.Resolve(ctx, before, "", "shadow.json"); err == nil {
		t.Fatal("old token still resolves")
	}
	if _, err := s.Resolve(ctx, next, "", "shadow.json"); err != nil {
		t.Fatal(err)
	}
}

func TestLxMusicExportIsDescriptorNotBundleDriver(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	body := `{"schema":"shadow.lx-music/v1","name":"Demo LX","version":5,"apiUrl":"https://music.example.com","scriptPath":"runtime/lx-music/demo.js"}`
	src, err := s.Import(ctx, Input{Name: "LX", Content: body})
	if err != nil {
		t.Fatal(err)
	}
	if src.Protocol != "lx-music" || !src.Enabled {
		t.Fatalf("lx import: %+v", src)
	}
	set, err := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-music")
	if err != nil || set.CurrentPublication == "" {
		t.Fatalf("music aggregate: %v %+v", err, set)
	}
	pub, err := store.Get[model.Publication](ctx, s.DB.Pool, "publications", set.CurrentPublication)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pub.Artifacts["lx-music/sources.json"]; !ok {
		t.Fatal("missing lx-music descriptor export")
	}
	warn := pub.FormatWarnings["lx-music/sources.json"]
	if warn == "" || !strings.Contains(strings.ToLower(warn), "descriptor") {
		t.Fatalf("missing honesty warning: %#v", pub.FormatWarnings)
	}
	shadow := pub.Artifacts["shadow.json"].Body
	if strings.Contains(shadow, `"driver":"lx-music"`) {
		t.Fatal("lx-music must not appear as Bundle playback driver")
	}
	_ = time.Now()
}
