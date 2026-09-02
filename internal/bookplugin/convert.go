package bookplugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/security"
)

func text(m map[string]any, k string) string        { v, _ := m[k].(string); return strings.TrimSpace(v) }
func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

// Convert reports every rejected rule independently. One unsupported rule never hides
// supported siblings, and no unsupported expression is silently made executable.
func Convert(protocol string, entries []json.RawMessage, sourceID string) Report {
	out := Report{Schema: "shadow.hub.plugins/v1", Entries: []Entry{}}
	seen := map[string]bool{}
	for _, data := range entries {
		e := Entry{Upstream: protocol, SourceID: sourceID, Blockers: []string{}, Warnings: []string{}}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			e.Blockers = append(e.Blockers, "invalid JSON")
		}
		r := Recipe{Schema: "shadow.book.recipe/v1", MaxPages: 50, MinIntervalMS: 1200}
		switch protocol {
		case "relay-book":
			if err := json.Unmarshal(data, &r); err != nil {
				e.Blockers = append(e.Blockers, "invalid recipe")
			}
		case "legado-book":
			r.Name = text(m, "bookSourceName")
			r.BaseURL = text(m, "bookSourceUrl")
			for _, key := range []string{"loginUrl", "loginUi", "loginCheckJs", "jsLib", "header", "bookSourceComment"} {
				if key == "bookSourceComment" {
					continue
				}
				if text(m, key) != "" {
					e.Blockers = append(e.Blockers, key+": requires manual authentication/script handling")
				}
			}
			r.Search.URL = normalizeTemplate(text(m, "searchUrl"))
			if strings.Contains(r.Search.URL, ",{") {
				e.Blockers = append(e.Blockers, "searchUrl request options require a manual recipe")
			}
			srch, detail, toc, chapter := obj(m, "ruleSearch"), obj(m, "ruleBookInfo"), obj(m, "ruleToc"), obj(m, "ruleContent")
			r.Search.List = parseSelector(text(srch, "bookList"), false, &e)
			r.Search.Fields = mapFields(srch, map[string]string{"name": "name", "author": "author", "bookUrl": "bookUrl", "coverUrl": "coverUrl", "intro": "intro", "kind": "kind", "lastChapter": "lastChapter", "wordCount": "wordCount"}, &e)
			r.Detail.Fields = mapFields(detail, map[string]string{"name": "name", "author": "author", "coverUrl": "coverUrl", "intro": "intro", "kind": "kind", "lastChapter": "lastChapter", "wordCount": "wordCount", "tocUrl": "tocUrl"}, &e)
			if text(detail, "init") != "" {
				e.Blockers = append(e.Blockers, "ruleBookInfo.init needs manual conversion")
			}
			r.TOC.List = parseSelector(text(toc, "chapterList"), false, &e)
			r.TOC.Fields = mapFields(toc, map[string]string{"title": "chapterName", "chapterUrl": "chapterUrl"}, &e)
			r.TOC.Next = parseSelector(text(toc, "nextTocUrl"), true, &e)
			r.Chapter.Fields = mapFields(chapter, map[string]string{"content": "content", "title": "title"}, &e)
			if sel, ok := r.Chapter.Fields["content"]; ok && sel.Path == "" {
				sel.HTML = true
				sel.Attr = ""
				r.Chapter.Fields["content"] = sel
			}
			r.Chapter.Next = parseSelector(text(chapter, "nextContentUrl"), true, &e)
			if text(chapter, "replaceRegex") != "" {
				e.Warnings = append(e.Warnings, "replaceRegex is not executed; use a reviewed native cleanup rule")
			}
			if v, ok := toc["isReverse"].(bool); ok {
				r.TOC.Reverse = v
			}
		case "so-novel":
			r.Name = text(m, "name")
			r.BaseURL = text(m, "url")
			r.Language = text(m, "language")
			if disabled, _ := m["disabled"].(bool); disabled {
				e.Blockers = append(e.Blockers, "upstream rule is disabled")
			}
			srch, detail, toc, chapter := obj(m, "search"), obj(m, "book"), obj(m, "toc"), obj(m, "chapter")
			if required, _ := srch["relayRequiresCredentials"].(bool); required {
				e.Blockers = append(e.Blockers, "upstream cookies were removed; configure a manual authenticated plugin")
			}
			if disabled, _ := srch["disabled"].(bool); disabled {
				e.Blockers = append(e.Blockers, "upstream search is disabled")
			}
			r.Search.URL = normalizeTemplate(text(srch, "url"))
			r.Search.Method = strings.ToUpper(text(srch, "method"))
			if text(srch, "cookies") != "" && text(srch, "cookies") != "{}" {
				e.Blockers = append(e.Blockers, "search.cookies must be configured in the runtime manually")
			}
			r.Search.Form = parseForm(text(srch, "data"), &e)
			r.Search.List = parseSelector(text(srch, "result"), false, &e)
			r.Search.Fields = mapFields(srch, map[string]string{"name": "bookName", "author": "author", "kind": "category", "lastChapter": "latestChapter", "wordCount": "wordCount"}, &e)
			r.Search.Fields["bookUrl"] = parseSelector(text(srch, "bookName"), true, &e)
			r.Detail.Fields = mapFields(detail, map[string]string{"name": "bookName", "author": "author", "coverUrl": "coverUrl", "intro": "intro", "kind": "category", "lastChapter": "latestChapter"}, &e)
			// so-novel uses OpenGraph metadata when explicit detail rules are absent.
			for key, property := range map[string]string{"name": "og:novel:book_name", "author": "og:novel:author", "coverUrl": "og:image", "intro": "og:description", "lastChapter": "og:novel:latest_chapter_name"} {
				if empty(r.Detail.Fields[key]) {
					r.Detail.Fields[key] = Selector{CSS: `meta[property="` + property + `"]`, Attr: "content"}
				}
			}
			for key, val := range map[string]string{"book.url": text(detail, "url"), "toc.url": text(toc, "url"), "toc.baseUri": text(toc, "baseUri")} {
				if val != "" {
					e.Blockers = append(e.Blockers, key+": custom URL rewrite requires a manual recipe")
				}
			}
			r.TOC.List = parseSelector(text(toc, "item"), false, &e)
			r.TOC.Fields = map[string]Selector{"title": {CSS: "$self"}, "chapterUrl": {CSS: "$self", Attr: "href"}}
			r.TOC.Next = parseSelector(text(toc, "nextPage"), true, &e)
			r.TOC.Reverse, _ = toc["isDesc"].(bool)
			r.Chapter.Fields = mapFields(chapter, map[string]string{"content": "content", "title": "title"}, &e)
			content := r.Chapter.Fields["content"]
			if content.Path == "" {
				content.HTML = true
				content.Attr = ""
			}
			r.Chapter.Fields["content"] = content
			r.Chapter.Next = parseSelector(text(chapter, "nextPage"), true, &e)
			r.Chapter.RemoveCSS = text(chapter, "filterTag")
			if text(chapter, "filterTxt") != "" {
				e.Warnings = append(e.Warnings, "filterTxt regex is not executed; host HTML cleanup is retained")
			}
			if crawl := obj(m, "crawl"); crawl != nil {
				if n, ok := crawl["minInterval"].(float64); ok {
					r.MinIntervalMS = max(1200, int(n*1000))
				}
			}
		default:
			e.Blockers = append(e.Blockers, "only Legado, so-novel and relay-book rules can be converted")
		}
		e.Name = r.Name
		base, _ := url.Parse(r.BaseURL)
		if base != nil && len(r.Domains) == 0 {
			r.Domains = []string{base.Hostname()}
		}
		if err := Validate(&r); err != nil {
			e.Blockers = append(e.Blockers, err.Error())
		}
		// Stable across rule edits, so upgrades replace a plugin rather than duplicate it.
		e.ID = "relay_" + security.Hash([]byte(sourceID + "|" + r.BaseURL))[:20]
		if seen[e.ID] {
			e.Blockers = append(e.Blockers, "duplicate site in this source pack")
		}
		seen[e.ID] = true
		if len(e.Blockers) == 0 {
			e.Recipe = &r
			out.Supported++
			e.Warnings = append(e.Warnings, "conversion is structural; capture fixtures and run live smoke before relying on this plugin")
		} else {
			out.Unsupported++
		}
		out.Entries = append(out.Entries, e)
	}
	return out
}
func normalizeTemplate(s string) string {
	return strings.NewReplacer("{{key}}", "{keyword}", "{{page}}", "{page}", "%s", "{keyword}").Replace(s)
}
func mapFields(m map[string]any, keys map[string]string, e *Entry) map[string]Selector {
	out := map[string]Selector{}
	for dest, key := range keys {
		if value := text(m, key); value != "" {
			sel := parseSelector(value, strings.HasSuffix(dest, "Url"), e)
			if dest == "coverUrl" && sel.Path == "" && !strings.Contains(value, "@") {
				if strings.Contains(value, "meta") {
					sel.Attr = "content"
				} else {
					sel.Attr = "src"
				}
			}
			out[dest] = sel
		}
	}
	return out
}
func parseSelector(value string, link bool, e *Entry) Selector {
	value = strings.TrimSpace(value)
	if value == "" {
		return Selector{}
	}
	value = strings.TrimPrefix(value, "@css:")
	value = strings.TrimPrefix(value, "@json:")
	if value == "text" {
		return Selector{CSS: "$self"}
	}
	if value == "href" || value == "src" || value == "content" {
		return Selector{CSS: "$self", Attr: value}
	}
	if strings.HasPrefix(value, "$") {
		s := Selector{Path: value}
		if err := validateSelector(s); err != nil {
			e.Blockers = append(e.Blockers, err.Error()+": "+value)
		}
		return s
	}
	for _, v := range []string{"@js:", "<js>", "##", "&&", "||", "{{", "@get:", "@put:", "@XPath:", "//"} {
		if strings.Contains(value, v) {
			e.Blockers = append(e.Blockers, "unsupported expression: "+truncate(value))
			return Selector{}
		}
	}
	parts := strings.Split(value, "@")
	s := Selector{CSS: parts[0]}
	if len(parts) > 2 {
		e.Blockers = append(e.Blockers, "chained default selectors require manual CSS conversion: "+truncate(value))
	}
	if len(parts) > 1 {
		switch parts[len(parts)-1] {
		case "text", "textNodes", "ownText":
			if parts[len(parts)-1] != "text" {
				e.Blockers = append(e.Blockers, "ownText/textNodes require manual conversion")
			}
		case "html":
			s.HTML = true
		default:
			s.Attr = parts[len(parts)-1]
		}
	} else if link {
		s.Attr = "href"
	}
	if s.CSS == "" {
		s.CSS = "$self"
	}
	// The most common Legado default selectors, without positional/chained semantics.
	if strings.HasPrefix(s.CSS, "class.") {
		s.CSS = "." + strings.TrimPrefix(s.CSS, "class.")
	}
	if strings.HasPrefix(s.CSS, "id.") {
		s.CSS = "#" + strings.TrimPrefix(s.CSS, "id.")
	}
	if strings.HasPrefix(s.CSS, "tag.") {
		s.CSS = strings.TrimPrefix(s.CSS, "tag.")
	}
	if regexp.MustCompile(`\.\d+(?:$|\.)`).MatchString(s.CSS) {
		e.Blockers = append(e.Blockers, "positional default selector requires manual CSS conversion")
	}
	if err := validateSelector(s); err != nil {
		e.Blockers = append(e.Blockers, err.Error())
	}
	return s
}
func truncate(s string) string {
	r := []rune(s)
	if len(r) > 100 {
		return string(r[:100]) + "…"
	}
	return s
}
func parseForm(s string, e *Entry) map[string]string {
	if s == "" || s == "{}" {
		return nil
	}
	out := map[string]string{}
	if strings.Contains(s, "@js:") {
		e.Blockers = append(e.Blockers, "scripted search body is unsupported")
		return out
	}
	var fields map[string]any
	if json.Unmarshal([]byte(s), &fields) == nil {
		for k, v := range fields {
			switch v := v.(type) {
			case string:
				out[k] = normalizeTemplate(v)
			case float64, bool:
				out[k] = fmt.Sprint(v)
			default:
				e.Blockers = append(e.Blockers, "nested form values are unsupported")
			}
		}
		return out
	}
	// so-novel's documented flat {searchkey: %s, searchtype: all} syntax.
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		e.Blockers = append(e.Blockers, "unsupported search form")
		return out
	}
	for _, entry := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}"), ",") {
		k, v, ok := strings.Cut(entry, ":")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || !attrName.MatchString(k) || strings.ContainsAny(v, "{}[]\"'") {
			e.Blockers = append(e.Blockers, "unsupported search form field")
			continue
		}
		out[k] = normalizeTemplate(v)
	}
	return out
}
