package adapter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
)

func TestProtocolFixtures(t *testing.T) {
	cases := []struct {
		name, body, hint string
		count            int
	}{
		{"m3u", `#EXTM3U
#EXTINF:-1 tvg-id="n1" group-title="News" tvg-logo="https://media.example.com/logo.png",News
https://media.example.com/live.m3u8`, "", 1},
		{"m3u", "News,#genre#\nRadio,https://media.example.com/radio.mp3", "", 1},
		{"tvbox", `{ // https://quoted.example.com
 "sites":[{"key":"safe","name":"CMS","type":1,"api":"https://media.example.com/api.php",},],}`, "", 1},
		{"tvbox", `{"urls":[{"name":"TV","url":"https://media.example.com/config.json"}]}`, "", 1},
		{"legado-book", `[{"bookSourceName":"Book","bookSourceUrl":"https://books.example.com","ruleSearch":{"name":"a@text"}}]`, "", 1},
		{"legado-rss", `[{"sourceName":"Feed","sourceUrl":"https://feeds.example.com"}]`, "", 1},
		{"legado-tts", `[{"name":"Voice","url":"https://voice.example.com/speak","contentType":"audio/mpeg"}]`, "", 1},
		{"rss", `<rss version="2.0"><channel><title>News</title><item><title>A</title><link>https://feeds.example.com/a</link></item></channel></rss>`, "", 1},
		{"atom", `<feed xmlns="http://www.w3.org/2005/Atom"><title>News</title><entry><title>A</title><link href="https://feeds.example.com/a"/></entry></feed>`, "", 1},
		{"opds1", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Books</title><entry><title>A</title><link rel="http://opds-spec.org/acquisition" href="https://books.example.com/a.epub"/></entry></feed>`, "", 1},
		{"opds2", `{"metadata":{"title":"Library"},"publications":[{"metadata":{"title":"Book"},"links":[{"href":"https://books.example.com/a.epub"}]}]}`, "", 1},
		{"opml", `<opml version="2.0"><body><outline text="News"><outline text="A" xmlUrl="https://feeds.example.com/feed.xml"/></outline></body></opml>`, "", 1},
		{"xmltv", `<tv><channel id="one"><display-name>One</display-name></channel><programme channel="one" start="20260101000000 +0000"><title>Show</title></programme></tv>`, "", 1},
		{"json-feed", `{"version":"https://jsonfeed.org/version/1.1","items":[{"id":"1","title":"News","url":"https://feeds.example.com/a"}]}`, "", 1},
		{"catalog", `{"entries":[{"name":"A","url":"https://media.example.com/config.json"}]}`, "", 1},
		{"shadow-bundle", `{"schema":"shadow.media.bundle/v1","providers":[{"id":"media","driver":"emby","mode":"direct-client","endpoint":"https://media.example.com"}]}`, "", 1},
		{"mihon-repo", `{"meta":{"name":"Example"},"sources":[]}`, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, e := Parse([]byte(tc.body), tc.hint, "")
			if e != nil {
				t.Fatal(e)
			}
			if n.Protocol != tc.name || len(n.Items) != tc.count {
				t.Fatalf("unexpected normalized result: %#v", n)
			}
		})
	}
}
func TestUnsafeInputRejected(t *testing.T) {
	for _, body := range []string{`{"urls":[{"name":"bad","url":"file:///etc/passwd"}]}`, `{"urls":[{"name":"bad","url":"https://example.com/?token=secret"}]}`, `{"sites":[{"key":"a","name":"a","type":1,"api":"https://example.com/","headers":{"Authorization":"secret"}}]}`, `<!DOCTYPE rss [<!ENTITY x SYSTEM "file:///etc/passwd">]><rss><channel><title>&x;</title></channel></rss>`, "#EXTM3U\n#EXTINF:-1,Missing", `{"urls":[}`, strings.Repeat(" ", 8<<20+1)} {
		t.Run(body[:min(25, len(body))], func(t *testing.T) {
			if _, e := Parse([]byte(body), "", ""); e == nil {
				t.Fatal("accepted unsafe input")
			}
		})
	}
}
func TestTVBoxNeverIncludesCode(t *testing.T) {
	n, e := Parse([]byte(`{"spider":"https://example.com/unknown.jar","sites":[{"key":"safe","name":"safe","type":1,"api":"https://example.com/api"},{"key":"jar","name":"jar","type":3,"api":"csp_Unknown"}]}`), "", "")
	if e != nil {
		t.Fatal(e)
	}
	if len(n.Items) != 1 || strings.Contains(string(n.Config), "spider") || strings.Contains(string(n.Config), "csp_") {
		t.Fatalf("unsafe site escaped: %s", n.Config)
	}
	if len(n.Warnings) == 0 {
		t.Fatal("missing exclusion explanation")
	}
}
func TestJSONCQuotedCommentsAndTrailingCommas(t *testing.T) {
	b, e := JSONC([]byte(`{"value":"https://example.com/a//b/*ok*/", /* comment */ "arr":["a,]",],}`))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	if m["value"] != "https://example.com/a//b/*ok*/" {
		t.Fatal("modified quoted text")
	}
	if _, e = JSONC([]byte(`{"x":1/*`)); e == nil {
		t.Fatal("accepted truncated comment")
	}
}
func TestLargeDeletionAndDomainChangeRequireReview(t *testing.T) {
	old := model.Normalized{Items: []model.Item{{ID: "1", Name: "One", URL: "https://a.example.com/one"}, {ID: "2", Name: "Two", URL: "https://a.example.com/two"}}}
	next := model.Normalized{Items: []model.Item{{ID: "1", Name: "One", URL: "https://b.example.com/one"}}}
	d := Difference(old, next)
	if !d.RequiresReview || d.Removed != 1 || d.Changed != 1 || len(d.DomainChanges) != 1 {
		t.Fatalf("unexpected diff %+v", d)
	}
}

func TestFeedLinksAreAbsoluteInPublishedSnapshot(t *testing.T) {
	n, e := Parse([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><title>News</title><entry><title>A</title><link href="/articles/a"/></entry></feed>`), "", "https://feeds.example.com/index.xml")
	if e != nil {
		t.Fatal(e)
	}
	var body string
	if e = json.Unmarshal(n.Config, &body); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(body, `href="https://feeds.example.com/articles/a"`) {
		t.Fatal("relative link did not resolve", body)
	}
	if strings.Count(strings.Split(body, ">")[0], "xmlns=") > 1 {
		t.Fatal("duplicate root XML namespace", body)
	}
}
