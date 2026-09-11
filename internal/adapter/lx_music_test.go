package adapter

import (
	"strings"
	"testing"
)

func TestLXMusicDescriptor(t *testing.T) {
	body := `{"schema":"shadow.lx-music/v1","name":"Demo LX","version":5,"apiUrl":"https://music.example.com","scriptPath":"runtime/lx-music/demo.js"}`
	n, e := Parse([]byte(body), "", "")
	if e != nil {
		t.Fatal(e)
	}
	if n.Protocol != "lx-music" || len(n.Items) != 1 || n.Items[0].URL != "https://music.example.com" {
		t.Fatalf("unexpected: %+v", n)
	}
	if !strings.Contains(string(n.Config), "runtime/lx-music/demo.js") {
		t.Fatalf("config missing scriptPath: %s", n.Config)
	}
	if _, e := Parse([]byte(`{"schema":"shadow.lx-music/v1","name":"Bad","apiUrl":"https://music.example.com","scriptPath":"runtime/lx-music/demo.js","apiKey":"secret"}`), "", ""); e == nil {
		t.Fatal("expected reject embedded apiKey")
	}
}

func TestMusicPlaylistFromAudioM3U(t *testing.T) {
	body := "#EXTM3U\n#EXTINF:-1,Track\nhttps://audio.example.com/song.mp3\n#EXTINF:-1,Track2\nhttps://audio.example.com/b.flac"
	n, e := Parse([]byte(body), "", "")
	if e != nil {
		t.Fatal(e)
	}
	if n.Protocol != "music-playlist" || len(n.Items) != 2 {
		t.Fatalf("want music-playlist, got %+v", n)
	}
	// Explicit m3u hint keeps IPTV protocol even for audio URLs.
	n2, e := Parse([]byte(body), "m3u", "")
	if e != nil || n2.Protocol != "m3u" {
		t.Fatalf("m3u hint should stay m3u: %v %+v", e, n2)
	}
}
