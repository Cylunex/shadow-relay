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
		{"legado-book", nil, "legado"},
		{"legado-rss", nil, "legado"},
		{"so-novel", nil, "legado"},
		{"relay-book", nil, "legado"},
		{"mihon-repo", nil, "legado"},
		{"opds2", nil, "legado"},
		{"rss", nil, "rss"},
		{"atom", nil, "rss"},
		{"podcast", nil, "rss"},
		{"json-feed", nil, "rss"},
		{"emby", nil, "other"},
		{"jellyfin", nil, "other"},
		{"", []string{"video.live"}, "iptv"},
		{"", []string{"text.novel"}, "legado"},
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
			if info.MemberCount != 1 || info.Token == "" || len(info.SubscribeURLs) != 1 {
				t.Fatalf("aggregate info incomplete: %+v", info)
			}
			if info.SubscribeURLs[0] != "https://relay.example.com/p/"+info.Token+"/shadow.json" {
				t.Fatalf("subscribe url: %v", info.SubscribeURLs)
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
