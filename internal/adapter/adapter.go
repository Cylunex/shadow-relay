// Package adapter parses configuration as data. It never executes imported rules.
package adapter

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
)

type Description struct {
	Protocol     string   `json:"protocol"`
	MediaTypes   []string `json:"mediaTypes"`
	Capabilities []string `json:"capabilities"`
	Runtime      bool     `json:"runtime"`
}

var registry = []Description{
	{"legado-replace", []string{"text.novel"}, []string{"cleanup"}, true},
	{"so-novel", []string{"text.novel"}, []string{"search", "detail", "toc", "chapter"}, true},
	{"relay-book", []string{"text.novel"}, []string{"search", "detail", "toc", "chapter"}, true},
	{"podcast", []string{"audio.podcast"}, []string{"browse", "stream"}, false},
	{"m3u", []string{"video.live"}, []string{"live", "stream"}, false},
	{"xmltv", []string{"support.epg"}, []string{"epg"}, false},
	{"tvbox", []string{"video.movie", "video.series"}, []string{"browse", "search", "stream"}, false},
	{"legado-book", []string{"text.novel"}, []string{"search", "detail", "chapter"}, true},
	{"legado-rss", []string{"text.article"}, []string{"browse"}, true},
	{"legado-tts", []string{"speech.tts"}, []string{"tts"}, true},
	{"rss", []string{"text.article", "audio.podcast"}, []string{"browse"}, false},
	{"atom", []string{"text.article"}, []string{"browse"}, false},
	{"json-feed", []string{"text.article", "audio.podcast"}, []string{"browse"}, false},
	{"opml", []string{"text.article"}, []string{"browse"}, false},
	{"opds1", []string{"text.ebook"}, []string{"browse", "download"}, false},
	{"opds2", []string{"text.ebook"}, []string{"browse", "download"}, false},
	{"shadow-bundle", []string{}, []string{}, false},
	{"mihon-repo", []string{"image.comic"}, []string{"search", "page"}, true},
	{"catalog", []string{}, []string{}, false},
}

func Describe() []Description { return registry }
func Base(protocol string) (model.Normalized, error) {
	for _, d := range registry {
		if d.Protocol == protocol {
			return model.Normalized{Protocol: protocol, MediaTypes: d.MediaTypes, Capabilities: d.Capabilities, Items: []model.Item{}, Warnings: []string{}, RequiresRuntime: d.Runtime}, nil
		}
	}
	return model.Normalized{}, errors.New("unsupported protocol")
}
func str(v any) string            { s, _ := v.(string); return s }
func raw(v any) json.RawMessage   { b, _ := json.Marshal(v); return b }
func list(v any) []any            { x, _ := v.([]any); return x }
func object(v any) map[string]any { x, _ := v.(map[string]any); return x }
func resolve(base, s string) string {
	u, e := url.Parse(s)
	if e != nil {
		return s
	}
	if u.IsAbs() {
		return s
	}
	b, e := url.Parse(base)
	if e != nil || b.Host == "" {
		return s
	}
	return b.ResolveReference(u).String()
}
func checkItems(n *model.Normalized) error {
	if len(n.Items) > 20000 {
		return errors.New("too many entries (maximum 20000)")
	}
	for i := range n.Items {
		p := &n.Items[i]
		p.Name = strings.TrimSpace(p.Name)
		if len(p.Name) > 500 || strings.ContainsAny(p.Name+p.Group+p.ID, "\r\n\x00") {
			return errors.New("invalid entry name or identifier")
		}
		if p.URL != "" {
			if e := security.SafeURL(p.URL); e != nil {
				return e
			}
		}
		if p.Logo != "" {
			if e := security.SafeURL(p.Logo); e != nil {
				return e
			}
		}
	}
	return nil
}

func Parse(b []byte, hint, base string) (model.Normalized, error) {
	if len(b) == 0 || len(b) > 8<<20 {
		return model.Normalized{}, errors.New("configuration must be between 1 byte and 8 MiB")
	}
	b = bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
	var n model.Normalized
	var e error
	if bytes.HasPrefix(b, []byte("<")) {
		n, e = parseXML(b, hint, base)
	} else if bytes.HasPrefix(b, []byte("{")) || bytes.HasPrefix(b, []byte("[")) {
		n, e = parseJSON(b, hint, base)
	} else {
		if hint != "" && hint != "m3u" {
			return n, errors.New("text input requires M3U/TXT format")
		}
		n, e = parseM3U(string(b), base)
	}
	if e != nil {
		return n, e
	}
	if hint != "" && hint != n.Protocol {
		return n, fmt.Errorf("protocol mismatch: detected %s", n.Protocol)
	}
	if e = checkItems(&n); e != nil {
		return n, e
	}
	return n, nil
}

// JSONC removes comments/trailing commas outside string literals, preserving quoted URLs and escape sequences.
func JSONC(b []byte) ([]byte, error) {
	out := []byte{}
	quoted, escape := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if quoted {
			out = append(out, c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(b) {
			if b[i+1] == '/' {
				for i < len(b) && b[i] != '\n' {
					i++
				}
				out = append(out, '\n')
				continue
			}
			if b[i+1] == '*' {
				i += 2
				for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
					i++
				}
				if i+1 >= len(b) {
					return nil, errors.New("unterminated JSON comment")
				}
				i++
				out = append(out, ' ')
				continue
			}
		}
		out = append(out, c)
	}
	clean := []byte{}
	quoted = false
	escape = false
	for i, c := range out {
		if quoted {
			clean = append(clean, c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && strings.ContainsRune(" \n\r\t", rune(out[j])) {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		clean = append(clean, c)
	}
	return clean, nil
}
func parseJSON(b []byte, hint, base string) (model.Normalized, error) {
	b, e := JSONC(b)
	if e != nil {
		return model.Normalized{}, e
	}
	var v any
	if e = json.Unmarshal(b, &v); e != nil {
		return model.Normalized{}, errors.New("invalid JSON configuration")
	}
	// so-novel cookie-bearing entries stay visible as blocked conversion candidates;
	// secrets remain only in the encrypted raw snapshot, never normalized exports.
	scrubSoNovel(v)
	if e = security.ValidateDocument(raw(v)); e != nil {
		return model.Normalized{}, e
	}
	o := object(v)
	a := list(v)
	protocol := hint
	if protocol == "" {
		switch {
		case o["schema"] == "shadow.book.recipe/v1":
			protocol = "relay-book"
		case o["schema"] == "shadow.podcast/v1":
			protocol = "podcast"
		case o["search"] != nil && o["toc"] != nil:
			protocol = "so-novel"
		case o["pattern"] != nil && o["replacement"] != nil:
			protocol = "legado-replace"
		case o["schema"] == "shadow.media.bundle/v1":
			protocol = "shadow-bundle"
		case o["sites"] != nil || o["urls"] != nil:
			protocol = "tvbox"
		case strings.Contains(str(o["version"]), "jsonfeed.org/version/"):
			protocol = "json-feed"
		case o["publications"] != nil || o["navigation"] != nil:
			protocol = "opds2"
		case o["meta"] != nil && (o["sources"] != nil || o["index_v2"] != nil):
			protocol = "mihon-repo"
		case len(a) > 0:
			first := object(a[0])
			switch {
			case first["pattern"] != nil && first["replacement"] != nil:
				protocol = "legado-replace"
			case first["schema"] == "shadow.book.recipe/v1":
				protocol = "relay-book"
			case first["search"] != nil && first["toc"] != nil:
				protocol = "so-novel"
			case first["pkg"] != nil && first["apk"] != nil:
				protocol = "mihon-repo"
			case first["bookSourceUrl"] != nil:
				protocol = "legado-book"
			case first["sourceUrl"] != nil:
				protocol = "legado-rss"
			case first["concurrentRate"] != nil || first["contentType"] != nil || first["ttsName"] != nil:
				protocol = "legado-tts"
			default:
				protocol = "catalog"
			}
		case o["bookSourceUrl"] != nil:
			protocol = "legado-book"
		case o["sourceUrl"] != nil:
			protocol = "legado-rss"
		case o["ttsName"] != nil || o["contentType"] != nil:
			protocol = "legado-tts"
		case o["entries"] != nil || o["sources"] != nil:
			protocol = "catalog"
		default:
			return model.Normalized{}, errors.New("unable to identify JSON protocol; select one explicitly")
		}
	}
	n, e := Base(protocol)
	if e != nil {
		return n, e
	}
	if slices.Contains([]string{"json-feed", "opds2", "shadow-bundle"}, protocol) {
		normalizeLinks(v, base)
		if e := security.ValidateDocument(raw(v)); e != nil {
			return n, e
		}
	}
	switch protocol {
	case "tvbox":
		safeSites := []any{}
		urls := []any{}
		for _, x := range list(o["urls"]) {
			x := object(x)
			u := resolve(base, str(x["url"]))
			if e := security.SafeURL(u); e != nil {
				return n, e
			}
			name := str(x["name"])
			n.Items = append(n.Items, model.Item{Name: name, URL: u, Group: "store"})
			urls = append(urls, map[string]any{"name": name, "url": u})
		}
		for _, x := range list(o["sites"]) {
			s := object(x)
			api := resolve(base, str(s["api"]))
			typ, _ := s["type"].(float64)
			if (typ != 0 && typ != 1) || security.SafeURL(api) != nil {
				n.Warnings = append(n.Warnings, "Excluded executable or non-HTTP TVBox site: "+str(s["name"]))
				continue
			}
			safe := map[string]any{"key": str(s["key"]), "name": str(s["name"]), "type": typ, "api": api}
			safeSites = append(safeSites, safe)
			n.Items = append(n.Items, model.Item{ID: str(s["key"]), Name: str(s["name"]), URL: api, Group: "site", Data: raw(safe)})
		}
		if o["spider"] != nil || o["parses"] != nil {
			n.Warnings = append(n.Warnings, "Spider binaries and executable parsers are excluded")
		}
		if len(n.Items) == 0 {
			return n, errors.New("TVBox has no safe HTTP sites or store URLs")
		}
		n.Config = raw(map[string]any{"sites": safeSites, "urls": urls})
	case "legado-book", "legado-rss", "legado-tts":
		if len(a) == 0 && o != nil {
			a = []any{o}
		}
		if len(a) == 0 {
			return n, errors.New("empty Legado configuration")
		}
		for _, x := range a {
			s := object(x)
			nameKey, urlKey := "bookSourceName", "bookSourceUrl"
			if protocol == "legado-rss" {
				nameKey, urlKey = "sourceName", "sourceUrl"
			}
			if protocol == "legado-tts" {
				nameKey, urlKey = "name", "url"
			}
			name := str(s[nameKey])
			if name == "" {
				name = str(s["ttsName"])
			}
			u := str(s[urlKey])
			if name == "" || u == "" {
				return n, errors.New("Legado entries require name and URL")
			}
			if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				if e := security.SafeURL(strings.Split(u, ",")[0]); e != nil {
					return n, e
				}
			}
			n.Items = append(n.Items, model.Item{Name: name, ID: security.Hash(raw(x))[:16], Data: raw(x)})
		}
		n.Config = raw(a)
		n.Warnings = append(n.Warnings, "Rules are data only; an isolated runtime or compatible client must execute them")
	case "catalog":
		if a == nil {
			a = list(o["entries"])
			if a == nil {
				a = list(o["sources"])
			}
		}
		for _, x := range a {
			s := object(x)
			name, u := str(s["name"]), str(s["url"])
			if name != "" && (s["m3u-link"] != nil || s["txt-link"] != nil) {
				for _, kind := range []string{"m3u", "txt"} {
					if link := str(s[kind+"-link"]); link != "" {
						n.Items = append(n.Items, model.Item{Name: name + " · " + strings.ToUpper(kind) + " · " + str(s["type"]), URL: catalogLink(base, link), Group: str(s["type"]), Data: raw(map[string]string{"protocol": "m3u"})})
					}
				}
				continue
			}
			if u == "" {
				u = str(s["sourceUrl"])
			}
			if u == "" {
				u = str(s["link"])
			}
			if name == "" || u == "" {
				return n, errors.New("catalog entries require name and url")
			}
			proto := str(s["protocol"])
			if proto == "" {
				proto = catalogProtocol(base)
			}
			n.Items = append(n.Items, model.Item{Name: name, URL: catalogLink(base, u), Group: str(s["category"]), Data: raw(map[string]any{"protocol": proto})})
		}
	case "json-feed":
		if !strings.Contains(str(o["version"]), "jsonfeed.org/version/") {
			return n, errors.New("invalid JSON Feed version")
		}
		for _, x := range list(o["items"]) {
			s := object(x)
			if str(s["id"]) == "" {
				return n, errors.New("JSON Feed items require id")
			}
			u := str(s["url"])
			for _, at := range list(s["attachments"]) {
				if u == "" {
					u = str(object(at)["url"])
				}
			}
			n.Items = append(n.Items, model.Item{ID: str(s["id"]), Name: str(s["title"]), URL: resolve(base, u)})
		}
		n.Config = raw(o)
	case "opds2":
		entries := append(list(o["publications"]), list(o["navigation"])...)
		for _, x := range entries {
			s := object(x)
			name := str(object(s["metadata"])["title"])
			if name == "" {
				name = str(s["title"])
			}
			u, rel, mime := str(s["href"]), str(s["rel"]), str(s["type"])
			for _, value := range list(s["links"]) {
				link := object(value)
				r := str(link["rel"])
				if u == "" || strings.Contains(r, "acquisition") {
					u = str(link["href"])
					rel = r
					mime = str(link["type"])
				}
			}
			if rel == "" {
				rel = "subsection"
			}
			n.Items = append(n.Items, model.Item{Name: name, URL: resolve(base, u), Rel: rel, MIME: mime, Data: raw(s)})
		}
		n.Config = raw(o)
	case "shadow-bundle":
		if o["schema"] != "shadow.media.bundle/v1" {
			return n, errors.New("invalid bundle schema")
		}
		for _, x := range list(o["providers"]) {
			s := object(x)
			if str(s["id"]) == "" || !slices.Contains([]string{"emby", "jellyfin", "tvbox", "m3u", "xmltv", "rss", "atom", "json-feed", "opml", "opds1", "opds2", "legado-hub", "suwayomi", "audiobookshelf", "dispatcharr", "miniflux"}, str(s["driver"])) || !slices.Contains([]string{"direct-client", "compiled", "runtime-backed"}, str(s["mode"])) {
				return n, errors.New("invalid bundle provider")
			}
			s["endpoint"] = resolve(base, str(s["endpoint"]))
			for _, value := range list(s["mediaTypes"]) {
				if mt := str(value); mt != "" && !slices.Contains(n.MediaTypes, mt) {
					n.MediaTypes = append(n.MediaTypes, mt)
				}
			}
			for _, value := range list(s["capabilities"]) {
				if c := str(value); c != "" && !slices.Contains(n.Capabilities, c) {
					n.Capabilities = append(n.Capabilities, c)
				}
			}
			n.Items = append(n.Items, model.Item{ID: str(s["id"]), Name: str(s["name"]), URL: resolve(base, str(s["endpoint"])), Data: raw(s)})
		}
		n.Config = raw(o)
	case "mihon-repo", "legado-replace", "so-novel", "relay-book", "podcast":
		return extraJSON(n, v, base)
	default:
		return n, errors.New("JSON is not supported for this protocol")
	}
	return n, nil
}

var attrRE = regexp.MustCompile(`([\w-]+)="([^"]*)"`)

func parseM3U(body, base string) (model.Normalized, error) {
	n, _ := Base("m3u")
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var pending *model.Item
	group := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			idx := -1
			quoted := false
			for i, c := range line {
				if c == '"' {
					quoted = !quoted
				}
				if c == ',' && !quoted {
					idx = i
					break
				}
			}
			if idx < 0 {
				return n, errors.New("M3U EXTINF requires a channel name")
			}
			item := model.Item{Name: strings.TrimSpace(line[idx+1:]), Group: group}
			for _, m := range attrRE.FindAllStringSubmatch(line[:idx], -1) {
				switch m[1] {
				case "tvg-id":
					item.ID = m[2]
				case "tvg-logo":
					item.Logo = resolve(base, m[2])
				case "group-title":
					item.Group = m[2]
				case "tvg-language":
					item.Language = m[2]
				case "tvg-country":
					item.Region = m[2]
				}
			}
			pending = &item
			continue
		}
		if strings.HasPrefix(line, "#") {
			if strings.Contains(strings.ToLower(line), "http-cookie") || strings.Contains(strings.ToLower(line), "authorization") {
				return n, errors.New("embedded playlist credentials are not allowed")
			}
			continue
		}
		if pending != nil {
			pending.URL = resolve(base, line)
			n.Items = append(n.Items, *pending)
			pending = nil
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) == 2 {
			if strings.TrimSpace(parts[1]) == "#genre#" {
				group = parts[0]
				continue
			}
			n.Items = append(n.Items, model.Item{Name: parts[0], URL: resolve(base, strings.TrimSpace(parts[1])), Group: group})
			continue
		}
		return n, errors.New("playlist entry has no EXTINF or TXT channel name")
	}
	if pending != nil {
		return n, errors.New("M3U channel is missing its URL")
	}
	if len(n.Items) == 0 {
		return n, errors.New("playlist contains no channels")
	}
	return n, nil
}

type xmlLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}
type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	URL      string    `xml:"xmlUrl,attr"`
	Children []outline `xml:"outline"`
}

func validateXML(b []byte) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	depth := 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return errors.New("invalid XML document")
		}
		switch t.(type) {
		case xml.Directive:
			return errors.New("XML directives and DTD are not allowed")
		case xml.StartElement:
			start := t.(xml.StartElement)
			for _, a := range start.Attr {
				if security.SensitiveKey(a.Name.Local) && a.Value != "" {
					return errors.New("XML credentials belong in the credential vault")
				}
				if strings.HasPrefix(a.Value, "http://") || strings.HasPrefix(a.Value, "https://") {
					if e := security.SafeURL(a.Value); e != nil {
						return e
					}
				}
			}
			depth++
			if depth > 64 {
				return errors.New("XML nesting exceeds limit")
			}
		case xml.ProcInst:
			if t.(xml.ProcInst).Target != "xml" {
				return errors.New("XML processing instructions are not allowed")
			}
		case xml.EndElement:
			depth--
		}
	}
}
func parseXML(b []byte, hint, base string) (model.Normalized, error) {
	if e := validateXML(b); e != nil {
		return model.Normalized{}, e
	}
	var err error
	b, err = normalizeXMLLinks(b, base)
	if err != nil {
		return model.Normalized{}, err
	}
	var root struct{ XMLName xml.Name }
	if e := xml.Unmarshal(b, &root); e != nil {
		return model.Normalized{}, e
	}
	p := ""
	switch root.XMLName.Local {
	case "tv":
		p = "xmltv"
	case "opml":
		p = "opml"
	case "rss":
		p = "rss"
	case "feed":
		p = "atom"
		if hint == "opds1" || bytes.Contains(b, []byte("opds-spec.org")) {
			p = "opds1"
		}
	default:
		return model.Normalized{}, errors.New("unsupported XML root")
	}
	n, _ := Base(p)
	switch p {
	case "xmltv":
		var doc struct {
			Channels []struct {
				ID   string `xml:"id,attr"`
				Name string `xml:"display-name"`
			} `xml:"channel"`
			Programs []struct {
				Channel string `xml:"channel,attr"`
				Start   string `xml:"start,attr"`
				Title   string `xml:"title"`
			} `xml:"programme"`
		}
		if e := xml.Unmarshal(b, &doc); e != nil {
			return n, e
		}
		ids := map[string]bool{}
		for _, c := range doc.Channels {
			if c.ID == "" {
				return n, errors.New("XMLTV channel needs an id")
			}
			ids[c.ID] = true
			n.Items = append(n.Items, model.Item{ID: c.ID, Name: c.Name})
		}
		for _, p := range doc.Programs {
			if !ids[p.Channel] || p.Start == "" || p.Title == "" {
				return n, errors.New("invalid XMLTV programme or missing channel")
			}
		}
		n.Config = raw(string(b))
	case "opml":
		var doc struct {
			Body struct {
				Outlines []outline `xml:"outline"`
			} `xml:"body"`
		}
		if e := xml.Unmarshal(b, &doc); e != nil {
			return n, e
		}
		var walk func([]outline, string)
		walk = func(os []outline, group string) {
			for _, o := range os {
				name := o.Text
				if name == "" {
					name = o.Title
				}
				if o.URL != "" {
					n.Items = append(n.Items, model.Item{Name: name, URL: resolve(base, o.URL), Group: group})
				}
				walk(o.Children, name)
			}
		}
		walk(doc.Body.Outlines, "")
	case "rss":
		var doc struct {
			Channel struct {
				Title string `xml:"title"`
				Items []struct {
					Title     string `xml:"title"`
					Link      string `xml:"link"`
					Guid      string `xml:"guid"`
					Enclosure struct {
						URL string `xml:"url,attr"`
					} `xml:"enclosure"`
				} `xml:"item"`
			} `xml:"channel"`
		}
		if e := xml.Unmarshal(b, &doc); e != nil {
			return n, e
		}
		if doc.Channel.Title == "" {
			return n, errors.New("RSS channel requires title")
		}
		for _, x := range doc.Channel.Items {
			u := x.Link
			if x.Enclosure.URL != "" {
				u = x.Enclosure.URL
			}
			n.Items = append(n.Items, model.Item{ID: x.Guid, Name: x.Title, URL: resolve(base, u)})
		}
		n.Config = raw(string(b))
	case "atom", "opds1":
		var doc struct {
			Title   string `xml:"title"`
			Entries []struct {
				ID    string    `xml:"id"`
				Title string    `xml:"title"`
				Links []xmlLink `xml:"link"`
			} `xml:"entry"`
		}
		if e := xml.Unmarshal(b, &doc); e != nil {
			return n, e
		}
		if doc.Title == "" {
			return n, errors.New("Atom feed requires title")
		}
		for _, x := range doc.Entries {
			u, rel, mime := "", "", ""
			for _, l := range x.Links {
				if u == "" || strings.Contains(l.Rel, "acquisition") {
					u = l.Href
					rel = l.Rel
					mime = l.Type
				}
			}
			n.Items = append(n.Items, model.Item{ID: x.ID, Name: x.Title, URL: resolve(base, u), Rel: rel, MIME: mime})
		}

		n.Config = raw(string(b))
	}
	return n, nil
}
func Difference(old, next model.Normalized) model.Diff {
	d := model.Diff{DomainChanges: []string{}}
	a, b := map[string]string{}, map[string]string{}
	domainsA, domainsB := map[string]bool{}, map[string]bool{}
	for _, pair := range []struct {
		n       model.Normalized
		m       map[string]string
		domains map[string]bool
	}{{old, a, domainsA}, {next, b, domainsB}} {
		for _, x := range pair.n.Items {
			key := x.ID
			if key == "" {
				key = x.Name
			}
			pair.m[key] = string(raw(x))
			if u, e := url.Parse(x.URL); e == nil && u.Hostname() != "" {
				pair.domains[u.Hostname()] = true
			}
		}
	}
	for k, v := range b {
		if prev, ok := a[k]; !ok {
			d.Added++
		} else if prev != v {
			d.Changed++
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			d.Removed++
		}
	}
	for h := range domainsB {
		if !domainsA[h] && len(domainsA) > 0 {
			d.DomainChanges = append(d.DomainChanges, h)
		}
	}
	sort.Strings(d.DomainChanges)
	d.RequiresReview = (len(a) > 0 && float64(d.Removed)/float64(len(a)) >= 0.3) || len(d.DomainChanges) > 0
	return d
}

func normalizeLinks(v any, base string) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if text, ok := value.(string); ok && text != "" && slices.Contains([]string{"url", "href", "endpoint", "home_page_url", "feed_url", "icon", "favicon", "external_url", "next_url"}, k) {
				x[k] = resolve(base, text)
			} else {
				normalizeLinks(value, base)
			}
		}
	case []any:
		for _, value := range x {
			normalizeLinks(value, base)
		}
	}
}
func normalizeXMLLinks(b []byte, base string) ([]byte, error) {
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	dec := xml.NewDecoder(bytes.NewReader(b))
	stack := []string{}
	for {
		t, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch x := t.(type) {
		case xml.StartElement:
			stack = append(stack, x.Name.Local)
			attrs := []xml.Attr{}
			for _, a := range x.Attr {
				if a.Name.Local != "xmlns" && a.Name.Space != "xmlns" {
					attrs = append(attrs, a)
				}
			}
			x.Attr = attrs
			for i, a := range x.Attr {
				if a.Value != "" && slices.Contains([]string{"href", "src", "url", "xmlUrl"}, a.Name.Local) {
					x.Attr[i].Value = resolve(base, a.Value)
					if e := security.SafeURL(x.Attr[i].Value); e != nil {
						return nil, e
					}
				}
			}
			t = x
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1] == "link" && strings.TrimSpace(string(x)) != "" {
				resolved := resolve(base, strings.TrimSpace(string(x)))
				if e := security.SafeURL(resolved); e != nil {
					return nil, e
				}
				t = xml.CharData(resolved)
			}
		}
		if e = enc.EncodeToken(t); e != nil {
			return nil, e
		}
	}
	if e := enc.Flush(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
