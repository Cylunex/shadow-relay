package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func TestPreferenceOverlaysSurviveIdentity(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	set, err := s.SaveSet(ctx, "", model.SourceSet{Name: "Live prefs"})
	if err != nil {
		t.Fatal(err)
	}
	key := ItemIdentity("m3u", "guide-one", "https://media.example.com/one.ts", "", "")
	pref, err := s.SavePreference(ctx, model.PreferenceOverlay{
		SetID: set.ID, ItemKey: key, Name: "My Channel", Group: "Favorites", Pinned: true,
	})
	if err != nil || pref.ID == "" {
		t.Fatal(err, pref)
	}
	items := ApplyPreferences([]model.Item{
		{ID: "guide-one", Name: "Original", URL: "https://media.example.com/one.ts", Group: "News"},
		{ID: "two", Name: "Hidden", URL: "https://media.example.com/two.ts"},
	}, []model.PreferenceOverlay{pref, {SetID: set.ID, ItemKey: ItemIdentity("m3u", "two", "https://media.example.com/two.ts", "", ""), Hidden: true}}, "m3u")
	if len(items) != 1 || items[0].Name != "My Channel" || items[0].Group != "Favorites" {
		t.Fatalf("overlay not applied: %+v", items)
	}
}

func TestSoftDeleteAndUndo(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	if err := s.SoftDeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if !s.IsSoftDeleted(ctx, src.ID) {
		t.Fatal("tombstone missing")
	}
	got, err := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if err != nil || got.Enabled || got.Health != "disabled" {
		t.Fatalf("soft delete should keep row disabled: %+v %v", got, err)
	}
	set, err := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", "aggregate-iptv")
	if err != nil || len(set.Members) != 0 {
		t.Fatalf("aggregate should drop soft-deleted member: %v %+v", err, set)
	}
	restored, err := s.UndoDeleteSource(ctx, src.ID)
	if err != nil || s.IsSoftDeleted(ctx, restored.ID) {
		t.Fatal(err)
	}
}

func TestPersonalCredentialResetRotatesTokens(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	_ = imported(t, s, playlist)
	before, err := s.EnsurePersonalCredential(ctx, "https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	oldTok := personalEntryToken(before, "iptv")
	if oldTok == "" {
		t.Fatalf("expected iptv token in %#v", before)
	}
	after, err := s.ResetPersonalCredential(ctx, "https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	newTok := personalEntryToken(after, "iptv")
	if oldTok == "" || newTok == "" || oldTok == newTok {
		t.Fatalf("reset did not rotate: %q -> %q", oldTok, newTok)
	}
}

func TestFilterProvidersByDrivers(t *testing.T) {
	bundle := map[string]any{
		"providers": []any{
			map[string]any{"id": "a", "driver": "m3u"},
			map[string]any{"id": "b", "driver": "emby"},
			map[string]any{"id": "c", "driver": "tvbox"},
		},
		"formatWarnings": map[string]any{},
	}
	out := FilterProvidersByDrivers(bundle, []string{"m3u", "tvbox"})
	providers := out["providers"].([]any)
	if len(providers) != 2 {
		t.Fatalf("want 2 providers, got %#v", providers)
	}
	warnings := out["formatWarnings"].(map[string]any)
	if warnings["providers"] == nil {
		t.Fatal("missing capability warning")
	}
}

func TestMusicPlaylistResolve(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	body := "#EXTM3U\n#EXTINF:-1,Song A\nhttps://music.example.com/a.mp3\n#EXTINF:-1,Song B\nhttps://music.example.com/b.flac\n"
	src, err := s.Import(ctx, Input{Name: "Mix", Content: body, Protocol: "music-playlist"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ResolveMusic(ctx, src.ID, "Song B", "")
	if err != nil || out["status"] != "ok" || out["playUrl"] != "https://music.example.com/b.flac" {
		t.Fatalf("resolve: %#v %v", out, err)
	}
}

func TestHubSyncLockRecoversAfterStale(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, ".relay-sync-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(lock, stale, stale); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverSyncLock(root, 15*time.Minute)
	if err != nil || !recovered {
		t.Fatalf("expected recovery: %v %v", recovered, err)
	}
	// Install should succeed after recovery path inside Install as well.
	report := bookplugin.Report{Schema: "shadow.hub.plugins/v1", SetID: "set_demo", GeneratedAt: model.Now(), Entries: nil}
	if _, err := bookplugin.Install(root, report); err != nil {
		t.Fatal(err)
	}
}

func TestItemIdentityScopesDedupe(t *testing.T) {
	a := ItemIdentity("legado-book", "book1", "https://a.example/x", "zh", "acct")
	b := ItemIdentity("legado-book", "book1", "https://a.example/x", "en", "acct")
	c := ItemIdentity("legado-book", "book1", "https://a.example/x", "zh", "other")
	if a == b || a == c || b == c {
		t.Fatal("identity must include lang and account scope")
	}
	_ = json.RawMessage{}
}

func personalEntryToken(payload map[string]any, typ string) string {
	switch entries := payload["entries"].(type) {
	case []map[string]any:
		for _, m := range entries {
			if m["type"] == typ {
				tok, _ := m["token"].(string)
				return tok
			}
		}
	case []any:
		for _, e := range entries {
			m, _ := e.(map[string]any)
			if m["type"] == typ {
				tok, _ := m["token"].(string)
				return tok
			}
		}
	}
	return ""
}
