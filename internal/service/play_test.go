package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func TestResolvePlayMusicPlaylistJSONAndRedirectGate(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	body := "#EXTM3U\n#EXTINF:-1,Song A\nhttps://music.example.com/a.mp3\n#EXTINF:-1,Song B\nhttps://music.example.com/b.flac\n"
	src, err := s.Import(ctx, Input{Name: "Mix", Content: body, Protocol: "music-playlist"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.ResolvePlay(ctx, src.ID, PlayResolveInput{Query: "Song B"})
	if err != nil || res.Status != "ok" || res.URL != "https://music.example.com/b.flac" || !res.AllowDirect {
		t.Fatalf("resolve: %+v %v", res, err)
	}
	if e := CanRedirect(res); e != nil {
		t.Fatal(e)
	}
	res.Headers = map[string]string{"Authorization": "secret-token"}
	res.AllowDirect = false
	if e := CanRedirect(res); e == nil {
		t.Fatal("redirect allowed with required headers")
	}
}

func TestResolvePlayRejectsPrivateLocation(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	body := "#EXTM3U\n#EXTINF:-1,Bad\nhttp://127.0.0.1/secret.ts\n"
	src, err := s.Import(ctx, Input{Name: "Lan", Content: body})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ResolvePlay(ctx, src.ID, PlayResolveInput{Query: "Bad"})
	if !errors.Is(err, fetch.ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}

func TestResolvePlayDirectLinkUpstream(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	doc := `{"schema":"shadow.direct-link/v1","resolveTemplate":"https://api.example.com/resolve?id={id}","items":[{"id":"clip1","name":"Clip"}]}`
	src, err := s.Import(ctx, Input{Name: "Drive", Content: doc})
	if err != nil {
		t.Fatal(err)
	}
	s.Fetch = &fakeFetch{fn: func(u string) (fetch.Result, error) {
		if !strings.Contains(u, "clip1") {
			t.Fatalf("unexpected resolve URL %s", u)
		}
		if strings.Contains(u, "Authorization") || strings.Contains(strings.ToLower(u), "token=") {
			t.Fatal("credential leaked into resolve URL")
		}
		body, _ := json.Marshal(map[string]any{"url": "https://cdn.example.com/clip1.mp4?signature=abc", "expiresAt": "2099-01-01T00:00:00Z"})
		return fetch.Result{Status: 200, Body: body, ContentType: "application/json"}, nil
	}}
	res, err := s.ResolvePlay(ctx, src.ID, PlayResolveInput{ItemID: "clip1"})
	if err != nil || res.URL != "https://cdn.example.com/clip1.mp4?signature=abc" || !res.AllowDirect {
		t.Fatalf("%+v %v", res, err)
	}
	if res.ExpiresAt == "" {
		t.Fatal("missing expiresAt")
	}
	// Credential headers must not appear in Location path of redirect gate.
	if e := CanRedirect(res); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(security.RedactURL(res.URL), "signature") {
		t.Fatal("redact left signature")
	}
}

func TestResolvePlayEmbyProtocolRejected(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src, err := s.Import(ctx, Input{Name: "Live", Content: "#EXTM3U\n#EXTINF:-1,One\nhttps://media.example.com/one.ts"})
	if err != nil {
		t.Fatal(err)
	}
	row, err := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.Protocol = "emby"
	row.Mode = "direct-client"
	if err := s.DB.Write(ctx, func(tx *store.Tx) error {
		return store.Put(ctx, tx, "sources", row.ID, row)
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.ResolvePlay(ctx, src.ID, PlayResolveInput{Query: "One"})
	if !errors.Is(err, errPlayUnsupported) {
		t.Fatalf("expected unsupported, got %v", err)
	}
}
