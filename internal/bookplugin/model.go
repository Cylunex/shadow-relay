// Package bookplugin compiles declarative rules into native LegadoHub plugins.
// It never executes imported expressions or Python in the control plane.
package bookplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/security"
)

type Selector struct {
	CSS  string `json:"css,omitempty"`
	Path string `json:"path,omitempty"`
	Attr string `json:"attr,omitempty"`
	HTML bool   `json:"html,omitempty"`
}
type Stage struct {
	URL       string              `json:"url,omitempty"`
	Method    string              `json:"method,omitempty"`
	Form      map[string]string   `json:"form,omitempty"`
	List      Selector            `json:"list"`
	Fields    map[string]Selector `json:"fields"`
	Next      Selector            `json:"next"`
	Reverse   bool                `json:"reverse,omitempty"`
	RemoveCSS string              `json:"removeCss,omitempty"`
}
type Recipe struct {
	ProxyMode     string   `json:"proxyMode,omitempty"`
	Schema        string   `json:"schema"`
	Name          string   `json:"name"`
	BaseURL       string   `json:"baseUrl"`
	Domains       []string `json:"domains"`
	Language      string   `json:"language,omitempty"`
	Search        Stage    `json:"search"`
	Detail        Stage    `json:"detail"`
	TOC           Stage    `json:"toc"`
	Chapter       Stage    `json:"chapter"`
	MaxPages      int      `json:"maxPages"`
	MinIntervalMS int      `json:"minIntervalMs"`
	SmokeKeyword  string   `json:"smokeKeyword,omitempty"`
}
type Entry struct {
	ID       string   `json:"id"`
	SourceID string   `json:"sourceId,omitempty"`
	Name     string   `json:"name"`
	Upstream string   `json:"upstream"`
	Recipe   *Recipe  `json:"recipe,omitempty"`
	Blockers []string `json:"blockers"`
	Warnings []string `json:"warnings"`
}
type Report struct {
	SetID       string  `json:"setId,omitempty"`
	GeneratedAt string  `json:"generatedAt,omitempty"`
	Schema      string  `json:"schema"`
	Entries     []Entry `json:"entries"`
	Supported   int     `json:"supported"`
	Unsupported int     `json:"unsupported"`
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,95}$`)
var jsonPath = regexp.MustCompile(`^\$(?:\.[A-Za-z_][\w-]*|\[(?:[0-9]+|\*)\])*$`)
var attrName = regexp.MustCompile(`^[A-Za-z_][\w:-]*$`)

func Validate(r *Recipe) error {
	if !slices.Contains([]string{"", "never", "always"}, r.ProxyMode) {
		return errors.New("proxyMode must be never or always")
	}
	if r.Schema != "shadow.book.recipe/v1" || strings.TrimSpace(r.Name) == "" || len(r.Name) > 160 {
		return errors.New("invalid recipe schema or name")
	}
	if e := security.SafeURL(r.BaseURL); e != nil {
		return e
	}
	u, _ := url.Parse(r.BaseURL)
	if u.RawQuery != "" {
		return errors.New("recipe base URL cannot have query parameters")
	}
	if len(r.Domains) == 0 {
		r.Domains = []string{u.Hostname()}
	}
	if len(r.Domains) > 8 || !slices.Contains(r.Domains, u.Hostname()) {
		return errors.New("recipe domains must include base hostname (maximum 8)")
	}
	for _, d := range r.Domains {
		if !regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(d) || strings.Contains(d, "..") {
			return errors.New("invalid recipe domain")
		}
	}
	if r.MaxPages == 0 {
		r.MaxPages = 50
	}
	if r.MaxPages < 1 || r.MaxPages > 100 {
		return errors.New("maxPages must be 1–100")
	}
	if r.MinIntervalMS == 0 {
		r.MinIntervalMS = 1200
	}
	if r.MinIntervalMS < 1200 || r.MinIntervalMS > 60000 {
		return errors.New("minIntervalMs must be 1200–60000")
	}
	if len(r.SmokeKeyword) > 120 {
		return errors.New("smoke keyword is too long")
	}
	if r.Search.URL == "" || empty(r.Search.List) || empty(r.Search.Fields["name"]) || empty(r.Search.Fields["bookUrl"]) || empty(r.Detail.Fields["name"]) || empty(r.TOC.List) || empty(r.TOC.Fields["chapterUrl"]) || empty(r.TOC.Fields["title"]) || empty(r.Chapter.Fields["content"]) {
		return errors.New("search, book URL, TOC and chapter content selectors are required")
	}
	for _, stage := range []*Stage{&r.Search, &r.Detail, &r.TOC, &r.Chapter} {
		if stage.Method == "" {
			stage.Method = "GET"
		}
		stage.Method = strings.ToUpper(stage.Method)
		if stage.Method != "GET" && stage.Method != "POST" {
			return errors.New("only GET and form POST are supported")
		}
		if stage != &r.Search && (stage.URL != "" || len(stage.Form) > 0 || stage.Method != "GET") {
			return errors.New("only search may declare a URL or form; later stages follow extracted links")
		}
		if len(stage.Form) > 32 || len(stage.Fields) > 16 {
			return errors.New("recipe has too many fields")
		}
		if stage.Method == "GET" && len(stage.Form) > 0 {
			return errors.New("form fields require POST search")
		}
		for key := range stage.Fields {
			if !slices.Contains([]string{"name", "author", "bookUrl", "coverUrl", "intro", "kind", "lastChapter", "wordCount", "tocUrl", "title", "chapterUrl", "content"}, key) {
				return errors.New("unsupported recipe output field")
			}
		}
		for key := range stage.Form {
			if !attrName.MatchString(key) || security.SensitiveKey(key) {
				return errors.New("invalid or credential-bearing form field")
			}
		}
		if stage.URL != "" {
			target := template(stage.URL, "example", 1)
			absolute := u.ResolveReference(mustURL(target))
			if e := security.SafeURL(absolute.String()); e != nil {
				return e
			}
			if !slices.Contains(r.Domains, absolute.Hostname()) {
				return errors.New("search URL is outside declared domains")
			}
		}
		for _, v := range stage.Form {
			if len(v) > 4096 || invalidTemplate(v) {
				return errors.New("unsupported form template")
			}
		}
		if invalidTemplate(stage.URL) {
			return errors.New("unsupported URL template; use {keyword} and {page}")
		}
		if strings.Contains(stage.URL, "@js:") || strings.Contains(stage.URL, "<js>") {
			return errors.New("scripted search URLs require a manual plugin")
		}
		for _, sel := range append([]Selector{stage.List, stage.Next}, values(stage.Fields)...) {
			if e := validateSelector(sel); e != nil {
				return e
			}
		}
		if stage.RemoveCSS != "" {
			if e := validateSelector(Selector{CSS: stage.RemoveCSS}); e != nil {
				return e
			}
		}
	}
	b, _ := json.Marshal(r)
	return security.ValidateDocument(b)
}
func mustURL(s string) *url.URL {
	u, e := url.Parse(s)
	if e != nil {
		return &url.URL{Path: s}
	}
	return u
}
func template(s, k string, p int) string {
	return strings.NewReplacer("{keyword}", url.QueryEscape(k), "{page}", fmt.Sprint(p)).Replace(s)
}
func invalidTemplate(s string) bool {
	s = strings.NewReplacer("{keyword}", "", "{page}", "").Replace(s)
	return strings.ContainsAny(s, "{}\x00\r\n")
}
func values(m map[string]Selector) []Selector {
	out := []Selector{}
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
func empty(s Selector) bool { return s.CSS == "" && s.Path == "" && s.Attr == "" && !s.HTML }
func validateSelector(s Selector) error {
	if len(s.CSS)+len(s.Path) > 2000 || (s.CSS != "" && s.Path != "") {
		return errors.New("invalid selector")
	}
	if s.Path != "" && !jsonPath.MatchString(s.Path) {
		return errors.New("JSONPath supports fields, numeric indexes and [*] only")
	}
	if s.Attr != "" && !attrName.MatchString(s.Attr) {
		return errors.New("invalid selector attribute")
	}
	if s.Path != "" && (s.Attr != "" || s.HTML) {
		return errors.New("JSONPath cannot use HTML attributes")
	}
	for _, x := range []string{"@js:", "<js>", "javascript:", "##", "&&", "||", "{{", "}}", "@get:", "@put:", "@web", "//"} {
		if strings.Contains(s.CSS, x) {
			return errors.New("selector needs a manual plugin or external parser")
		}
	}
	if strings.ContainsAny(s.CSS, "\r\n\x00") {
		return errors.New("invalid CSS selector")
	}
	return nil
}
