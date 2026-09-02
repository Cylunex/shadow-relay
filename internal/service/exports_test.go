package service

import (
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
)

func TestChannelOverridesAreAppliedToBothIndependentAndAggregateArtifacts(t *testing.T) {
	items := []model.Item{{ID: "one", Name: "Original", URL: "https://example.com/one.m3u8"}, {ID: "two", Name: "Hidden", URL: "https://example.com/two.m3u8"}}
	set := model.SourceSet{ID: "set_test", Name: "Live", ChannelRules: []model.ChannelRule{{SourceID: "src_test", Match: "one", Name: "Renamed", Group: "Favorites", TVGID: "guide-one"}, {SourceID: "src_test", Match: "two", Hide: true}}}
	src := model.Source{ID: "src_test", Name: "Live", Mode: "compiled", Protocol: "m3u", MediaTypes: []string{"video.live"}, Health: "healthy", Score: 100}
	pub, e := Compile(set, []selected{{Source: src, Revision: model.Revision{ID: "rev_test", Normalized: model.Normalized{Protocol: "m3u", Items: items}}, Member: model.Member{SourceID: src.ID}, Driver: "m3u"}}, map[string]string{})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"sources/src_test/live.m3u", "iptv/live.m3u", "iptv/live.txt"} {
		body := pub.Artifacts[path].Body
		if !strings.Contains(body, "Renamed") || strings.Contains(body, "Hidden") || strings.Contains(body, "two.m3u8") {
			t.Fatalf("channel override missing from %s", path)
		}
	}
	if !strings.Contains(pub.Artifacts["iptv/live.m3u"].Body, "guide-one") {
		t.Fatal("EPG mapping missing")
	}
}

func TestPodcastPublicationScopesEpisodeGUIDsAndProvidesLinks(t *testing.T) {
	items := []selected{}
	for _, id := range []string{"src_one", "src_two"} {
		src := model.Source{ID: id, Name: id, Protocol: "podcast", Mode: "compiled", Health: "healthy", Score: 100, MediaTypes: []string{"audio.podcast"}}
		n := model.Normalized{Protocol: "podcast", Config: jsonBytes(map[string]string{"title": id}), Items: []model.Item{{ID: "episode-1", Name: "Episode", URL: "https://example.com/" + id + ".mp3", MIME: "audio/mpeg", Data: jsonBytes(map[string]int{"length": 123})}}}
		items = append(items, selected{Source: src, Revision: model.Revision{ID: "rev_" + id, Normalized: n}, Member: model.Member{SourceID: id}, Driver: "podcast"})
	}
	pub, e := Compile(model.SourceSet{ID: "set_podcasts", Name: "Audio"}, items, map[string]string{})
	if e != nil {
		t.Fatal(e)
	}
	body := pub.Artifacts["podcasts/feed.xml"].Body
	for _, id := range []string{"src_one", "src_two"} {
		if !strings.Contains(body, id+":episode-1") {
			t.Fatal("aggregated episode lost its source-scoped identity")
		}
		path := "sources/" + id + "/podcast.xml"
		if !strings.Contains(pub.Artifacts[path].Body, "<link>"+BasePlaceholder+"/"+path+"</link>") {
			t.Fatal("podcast without a homepage did not get its published feed link")
		}
	}
}
