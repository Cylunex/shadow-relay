package bookplugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const staticLegado = `{"bookSourceName":"Example","bookSourceUrl":"https://example.com","searchUrl":"/search?q={{key}}","ruleSearch":{"bookList":".book","name":"a@text","bookUrl":"a@href"},"ruleBookInfo":{"name":"h1@text"},"ruleToc":{"chapterList":".toc a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":"#content@html"}}`

func TestConversionSeparatesUnsupportedRulesAndKeepsStableIDs(t *testing.T) {
	a := Convert("legado-book", []json.RawMessage{json.RawMessage(staticLegado)}, "src_test")
	if a.Supported != 1 || a.Unsupported != 0 {
		t.Fatalf("static rule rejected: %+v", a)
	}
	changed := strings.Replace(staticLegado, "#content@html", ".chapter@html", 1)
	b := Convert("legado-book", []json.RawMessage{json.RawMessage(changed)}, "src_test")
	if a.Entries[0].ID != b.Entries[0].ID || Version(*a.Entries[0].Recipe) == Version(*b.Entries[0].Recipe) {
		t.Fatal("identity must stay stable while recipe version changes")
	}
	bad := strings.Replace(staticLegado, "#content@html", "@js:java.getString()", 1)
	c := Convert("legado-book", []json.RawMessage{json.RawMessage(bad)}, "src_other")
	if c.Supported != 0 || c.Entries[0].Recipe != nil || len(c.Entries[0].Blockers) == 0 {
		t.Fatal("executable rule entered native plugin output")
	}
}
func TestArchiveDoesNotInterpolateRuleTextIntoPython(t *testing.T) {
	r := Convert("legado-book", []json.RawMessage{json.RawMessage(staticLegado)}, "src_test")
	r.Entries[0].Recipe.Name = "\"; unexpected() #"
	b, e := Archive(r, "2026-09-02T00:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "source.py") {
			found = true
			reader, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			_, _ = out.ReadFrom(reader)
			reader.Close()
			if out.String() != runner {
				t.Fatal("Python code contains interpolated user input")
			}
		}
	}
	if !found {
		t.Fatal("archive has no native plugin")
	}
}
func TestInstallProtectsManualPluginsAndRemovesOnlyOwnedDirectories(t *testing.T) {
	r := Convert("legado-book", []json.RawMessage{json.RawMessage(staticLegado)}, "src_test")
	r.SetID = "set_test"
	r.GeneratedAt = "2026-09-02T00:00:00Z"
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := r.Entries[0].ID
	if e := os.MkdirAll(filepath.Join(root, id), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := Install(root, r); e == nil {
		t.Fatal("manual directory overwritten")
	}
	if e := os.Remove(filepath.Join(root, id)); e != nil {
		t.Fatal(e)
	}
	if _, e := Install(root, r); e != nil {
		t.Fatal(e)
	}
	unchanged, e := Install(root, r)
	if e != nil || unchanged.Unchanged != 1 {
		t.Fatalf("idempotent sync failed: %+v %v", unchanged, e)
	}
	if e := os.Mkdir(filepath.Join(root, "manual_site"), 0700); e != nil {
		t.Fatal(e)
	}
	r.Entries = nil
	r.Supported = 0
	removed, e := Install(root, r)
	if e != nil || removed.Removed != 1 {
		t.Fatalf("ownership removal failed: %+v %v", removed, e)
	}
	if _, e := os.Stat(filepath.Join(root, "manual_site")); e != nil {
		t.Fatal("foreign directory removed")
	}
}
func TestRecipeRefusesScriptTemplatesAndExternalDomains(t *testing.T) {
	r := Convert("legado-book", []json.RawMessage{json.RawMessage(staticLegado)}, "src_test")
	v := *r.Entries[0].Recipe
	v.Search.URL = "https://other.example/search?q={keyword}"
	if Validate(&v) == nil {
		t.Fatal("search escaped declared domains")
	}
	v.Search.URL = "/search?q={{java.getString()}}"
	if Validate(&v) == nil {
		t.Fatal("script template accepted")
	}
	if validateSelector(Selector{Path: "$..items[?(@.id)]"}) == nil {
		t.Fatal("unsupported JSONPath accepted")
	}
	entry := r.Entries[0]
	entry.ID = "manual_site"
	if _, e := Files(entry, "2026-09-02T00:00:00Z"); e == nil {
		t.Fatal("plugin namespace escaped Relay ownership")
	}
}
