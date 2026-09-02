package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestYuancCatalogAndModernMihonDescriptor(t *testing.T) {
	n, e := Parse([]byte(`[{"name":"Demo","link":"../../rules.json","category":"小说"}]`), "", "https://example.com/data/legado/books.json")
	if e != nil || len(n.Items) != 1 {
		t.Fatalf("yuanc rejected: %+v %v", n, e)
	}
	var meta map[string]string
	_ = json.Unmarshal(n.Items[0].Data, &meta)
	if n.Items[0].URL != "https://example.com/rules.json" || meta["protocol"] != "legado-book" {
		t.Fatalf("wrong catalog mapping: %+v", n.Items)
	}
	n, e = Parse([]byte(`{"index_v2":"index.pb","meta":{"name":"Example","signingKeyFingerprint":"public-fingerprint"}}`), "", "https://example.com/repo.json")
	if e != nil || n.Protocol != "mihon-repo" || len(n.Items) != 1 || n.Items[0].URL != "https://example.com/index.pb" {
		t.Fatalf("modern descriptor rejected: %+v %v", n, e)
	}
	if !strings.Contains(string(n.Config), `"index_v2":"https://example.com/index.pb"`) {
		t.Fatal("relative upstream index was not normalized in the exported metadata")
	}
}

func TestYuancRootLinksAndSeparateLiveFormats(t *testing.T) {
	n, e := Parse([]byte(`[{"name":"Local","link":"data/legado/local.json"}]`), "catalog", "https://example.com/owner/repo/main/data/legado/books.json")
	if e != nil || len(n.Items) != 1 || n.Items[0].URL != "https://example.com/owner/repo/main/data/legado/local.json" {
		t.Fatalf("project-root catalog URL failed: %+v %v", n, e)
	}
	n, e = Parse([]byte(`[{"name":"Live","m3u-link":"https://example.com/live.m3u","txt-link":"https://example.com/live.txt","ipv-type":"ipv4"}]`), "catalog", "https://example.com/data/iptv/live.json")
	if e != nil || len(n.Items) != 2 {
		t.Fatalf("IPTV formats were lost: %+v %v", n, e)
	}
}
func TestSoNovelCredentialsAreRemovedAndBlockedExplicitly(t *testing.T) {
	n, e := Parse([]byte(`[{"name":"Example","url":"https://example.com","search":{"url":"/search?q=%s","cookies":"session=DO_NOT_PUBLISH","result":".book","bookName":"a"},"book":{},"toc":{"item":"a"},"chapter":{"content":"#content"}}]`), "", "https://example.com/rules.json")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(n.Config), "DO_NOT_PUBLISH") || !strings.Contains(string(n.Config), "relayRequiresCredentials") {
		t.Fatal("credential was exposed or conversion guard lost")
	}
}
func TestCleanupAndPodcastHaveDistinctProtocols(t *testing.T) {
	for _, tc := range []struct{ body, protocol string }{
		{`[{"name":"Spacing","pattern":"  ","replacement":" ","isRegex":false}]`, "legado-replace"},
		{`{"schema":"shadow.podcast/v1","title":"Audio","link":"https://example.com","episodes":[{"title":"Episode","url":"https://example.com/audio.mp3","type":"audio/mpeg","length":123}]}`, "podcast"},
	} {
		n, e := Parse([]byte(tc.body), "", "")
		if e != nil || n.Protocol != tc.protocol {
			t.Fatalf("%s: %s %v", tc.protocol, n.Protocol, e)
		}
	}
}
