package service

import (
	"context"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func TestAggregateTypeForBucketing(t *testing.T) {
	cases := []struct {
		protocol string
		media    []string
		want     string
	}{
		{"tvbox", nil, "tvbox"},
		{"m3u", nil, "iptv"},
		{"xmltv", nil, "iptv"},
		{"dispatcharr", nil, "iptv"},
		{"legado-book", nil, "novel"},
		{"legado-rss", nil, "rss"},
		{"so-novel", nil, "novel"},
		{"relay-book", nil, "novel"},
		{"mihon-repo", nil, "manga"},
		{"opds2", nil, "novel"},
		{"legado-tts", nil, "audiobook"},
		{"podcast", nil, "audiobook"},
		{"lx-music", nil, "music"},
		{"music-playlist", nil, "music"},
		{"rss", nil, "rss"},
		{"atom", nil, "rss"},
		{"json-feed", nil, "rss"},
		{"rss", []string{"audio.music"}, "music"},
		{"emby", nil, "other"},
		{"jellyfin", nil, "other"},
		{"", []string{"video.live"}, "iptv"},
		{"", []string{"text.novel"}, "novel"},
		{"", []string{"image.comic"}, "manga"},
		{"", []string{"speech.tts"}, "audiobook"},
		{"", []string{"audio.music"}, "music"},
		{"", []string{"audio.podcast"}, "rss"},
		{"unknown", nil, "other"},
	}
	for _, tc := range cases {
		if got := AggregateTypeFor(tc.protocol, tc.media); got != tc.want {
			t.Fatalf("%q/%v: got %s want %s", tc.protocol, tc.media, got, tc.want)
		}
	}
	if !IsAggregateSetID("aggregate-tvbox") || IsAggregateSetID("set_home") {
		t.Fatal("aggregate set id helper mismatch")
	}
}

func TestAggregateMembershipOnEnableDisableDelete(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	// Synthetic M3U body only — example.com, not a real catalog.
	src := imported(t, s, "#EXTM3U\n#EXTINF:-1,Demo\nhttps://media.example.com/demo.ts")
	if src.Protocol != "m3u" {
		t.Fatalf("expected m3u, got %s", src.Protocol)
	}
	approve(t, s, src)

	set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if e != nil {
		t.Fatal(e)
	}
	if len(set.Members) != 1 || set.Members[0].SourceID != src.ID {
		t.Fatalf("expected membership after enable: %+v", set.Members)
	}
	if set.CurrentPublication == "" {
		t.Fatal("aggregate should auto-publish after enable")
	}
	b, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", "binding_aggregate-iptv")
	if e != nil || b.SetID != "aggregate-iptv" {
		t.Fatalf("system binding missing: %v %+v", e, b)
	}
	infos, e := s.ListAggregates(ctx, "https://relay.example.com")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, info := range infos {
		if info.Slug == "aggregate-iptv" {
			found = true
			if info.MemberCount != 1 || info.Token == "" || len(info.SubscribeURLs) < 1 {
				t.Fatalf("aggregate info incomplete: %+v", info)
			}
			wantPrimary := "https://relay.example.com/p/" + info.Token + "/iptv/live.m3u"
			if info.SubscribeURLs[0] != wantPrimary {
				t.Fatalf("subscribe url primary: %v want %s", info.SubscribeURLs, wantPrimary)
			}
			if info.SubscribeByClient["iptv"] != wantPrimary {
				t.Fatalf("subscribeByClient: %+v", info.SubscribeByClient)
			}
		}
	}
	if !found {
		t.Fatal("aggregate-iptv missing from list")
	}

	if e := s.SourceAction(ctx, src.ID, "disable", ""); e != nil {
		t.Fatal(e)
	}
	set, e = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if e != nil {
		t.Fatal(e)
	}
	if len(set.Members) != 0 {
		t.Fatalf("expected removal on disable, got %+v", set.Members)
	}

	// Re-enable then delete: aggregate membership cleared automatically.
	if e := s.SourceAction(ctx, src.ID, "enable", ""); e != nil {
		t.Fatal(e)
	}
	if e := s.DeleteSource(ctx, src.ID); e != nil {
		t.Fatal(e)
	}
	set, e = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if e != nil {
		t.Fatal(e)
	}
	if len(set.Members) != 0 {
		t.Fatalf("expected removal on delete, got %+v", set.Members)
	}
}

func TestAggregateKeepsManualSetsIntact(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, "#EXTM3U\n#EXTINF:-1,Demo\nhttps://media.example.com/demo.ts")
	approve(t, s, src)
	manual, e := s.SaveSet(ctx, "", model.SourceSet{Name: "Manual Home", Members: []model.Member{{SourceID: src.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.SourceAction(ctx, src.ID, "disable", ""); e != nil {
		t.Fatal(e)
	}
	manual, e = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", manual.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(manual.Members) != 1 || manual.Members[0].SourceID != src.ID {
		t.Fatalf("manual set should keep membership: %+v", manual.Members)
	}
	agg, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if e != nil {
		t.Fatal(e)
	}
	if len(agg.Members) != 0 {
		t.Fatalf("aggregate should drop on disable: %+v", agg.Members)
	}
}

func TestReconcileAggregatesRebuckets(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, `[{"bookSourceName":"Example book","bookSourceUrl":"https://books.example.com","ruleSearch":{"name":"a@text"}}]`)
	approve(t, s, src)
	set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-novel")
	if e != nil || len(set.Members) != 1 {
		t.Fatalf("expected novel membership: %v %+v", e, set)
	}
	// Simulate legacy bucket left-over then reconcile.
	_ = s.DB.Write(ctx, func(tx *store.Tx) error {
		legacy := model.SourceSet{ID: "aggregate-legado", Name: "legacy", Members: []model.Member{{SourceID: src.ID}}, UpdatedAt: model.Now()}
		return store.Put(ctx, tx, "source_sets", legacy.ID, legacy)
	})
	result, e := s.ReconcileAggregates(ctx, "https://relay.example.com")
	if e != nil {
		t.Fatal(e)
	}
	if result.EnabledSources != 1 || result.Memberships != 1 {
		t.Fatalf("reconcile counts: %+v", result)
	}
	legacy, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-legado")
	if e != nil || len(legacy.Members) != 0 {
		t.Fatalf("legacy should be cleared: %v %+v", e, legacy)
	}
	set, e = store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-novel")
	if e != nil || len(set.Members) != 1 || set.Members[0].SourceID != src.ID {
		t.Fatalf("novel membership after reconcile: %v %+v", e, set)
	}
}

func TestAggregateSubscribeSelection(t *testing.T) {
	avail := map[string]bool{
		"legado/books.json": true,
		"hub/plugins.json":  true,
		"shadow.json":       true,
		"tvbox/store.json":  true,
		"iptv/live.m3u":     true,
	}
	urls, by := aggregateSubscribe("https://relay.example.com", "tok", "novel", avail)
	if len(urls) < 1 || urls[0] != "https://relay.example.com/p/tok/legado/books.json" {
		t.Fatalf("novel primary: %v", urls)
	}
	if by["legado"] != urls[0] || by["hub"] == "" || by["shadowMedia"] == "" {
		t.Fatalf("novel byClient: %+v", by)
	}
	urls, by = aggregateSubscribe("https://relay.example.com", "tok", "tvbox", avail)
	if urls[0] != "https://relay.example.com/p/tok/tvbox/store.json" || by["tvbox"] != urls[0] {
		t.Fatalf("tvbox: %v %+v", urls, by)
	}
	urls, by = aggregateSubscribe("", "tok", "iptv", avail)
	if urls[0] != "/p/tok/iptv/live.m3u" || by["iptv"] != urls[0] {
		t.Fatalf("iptv relative: %v %+v", urls, by)
	}
	urls, by = aggregateSubscribe("https://relay.example.com", "tok", "other", map[string]bool{"shadow.json": true})
	if len(urls) != 1 || urls[0] != "https://relay.example.com/p/tok/shadow.json" {
		t.Fatalf("other: %v", urls)
	}
	// Missing client export should not invent URLs.
	urls, by = aggregateSubscribe("https://relay.example.com", "tok", "novel", map[string]bool{"shadow.json": true})
	if len(urls) != 1 || by["legado"] != "" || urls[0] != "https://relay.example.com/p/tok/shadow.json" {
		t.Fatalf("novel without books: %v %+v", urls, by)
	}
	// Unknown publication: assume type defaults so novel still leads with legado.
	urls, by = aggregateSubscribe("https://relay.example.com", "tok", "novel", nil)
	if urls[0] != "https://relay.example.com/p/tok/legado/books.json" || by["legado"] != urls[0] {
		t.Fatalf("novel defaults: %v %+v", urls, by)
	}
	urls, by = aggregateSubscribe("https://relay.example.com", "tok", "music", nil)
	if by["lxMusic"] == "" && by["playlist"] == "" {
		t.Fatalf("music defaults: %v %+v", urls, by)
	}
}

func TestListAggregatesNovelAdvertisesLegado(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, `[{"bookSourceName":"Example book","bookSourceUrl":"https://books.example.com","ruleSearch":{"name":"a@text"}}]`)
	approve(t, s, src)
	infos, e := s.ListAggregates(ctx, "https://relay.example.com")
	if e != nil {
		t.Fatal(e)
	}
	for _, info := range infos {
		if info.Slug != "aggregate-novel" {
			continue
		}
		if info.MemberCount != 1 || info.Token == "" {
			t.Fatalf("novel aggregate incomplete: %+v", info)
		}
		want := "https://relay.example.com/p/" + info.Token + "/legado/books.json"
		if len(info.SubscribeURLs) < 1 || info.SubscribeURLs[0] != want {
			t.Fatalf("novel subscribeUrls: %v", info.SubscribeURLs)
		}
		if info.SubscribeByClient["legado"] != want {
			t.Fatalf("subscribeByClient legado: %+v", info.SubscribeByClient)
		}
		return
	}
	t.Fatal("aggregate-novel missing")
}
