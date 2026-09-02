package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
)

func extraJSON(n model.Normalized, v any, base string) (model.Normalized, error) {
	o, a := object(v), list(v)
	switch n.Protocol {
	case "legado-replace":
		if a == nil && o != nil {
			a = []any{o}
		}
		if len(a) == 0 {
			return n, errors.New("empty cleanup rules")
		}
		for _, value := range a {
			s := object(value)
			if str(s["name"]) == "" || str(s["pattern"]) == "" {
				return n, errors.New("cleanup rules require name and pattern")
			}
			n.Items = append(n.Items, model.Item{ID: security.Hash(raw(value))[:16], Name: str(s["name"]), Group: str(s["group"]), Data: raw(value)})
		}
		n.Config = raw(a)
		n.Warnings = append(n.Warnings, "Cleanup expressions are exported as data; only the reader executes them")
	case "so-novel", "relay-book":
		if a == nil && o != nil {
			a = []any{o}
		}
		if len(a) == 0 {
			return n, errors.New("empty book rules")
		}
		for _, value := range a {
			s := object(value)
			name, u := str(s["name"]), str(s["url"])
			if n.Protocol == "relay-book" {
				var r bookplugin.Recipe
				if e := json.Unmarshal(raw(value), &r); e != nil {
					return n, e
				}
				if e := bookplugin.Validate(&r); e != nil {
					return n, e
				}
				value = r
				u = r.BaseURL
			}
			if name == "" || u == "" {
				return n, errors.New("book rules require name and site URL")
			}
			if e := security.SafeURL(u); e != nil {
				return n, e
			}
			n.Items = append(n.Items, model.Item{ID: security.Hash([]byte(u))[:16], Name: name, URL: u, Data: raw(value)})
		}
		n.Config = raw(a)
		n.Warnings = append(n.Warnings, "Export native Hub plugins in the book workshop; unsupported expressions remain in the compatibility report")
	case "mihon-repo":
		if o["meta"] != nil {
			meta := object(o["meta"])
			name := str(meta["name"])
			if name == "" {
				return n, errors.New("repository metadata requires a name")
			}
			u := str(o["index_v2"])
			if u == "" {
				u = base
			}
			if u != "" {
				u = resolve(base, u)
				if e := security.SafeURL(u); e != nil {
					return n, e
				}
			}
			n.Items = append(n.Items, model.Item{ID: security.Hash([]byte(name + u))[:16], Name: name, URL: u, Group: "repository", Data: raw(meta)})
			if o["index_v2"] != nil {
				o["index_v2"] = u
				n.Warnings = append(n.Warnings, "This repository uses index.pb; Relay preserves its signed upstream index instead of rebuilding extension packages")
			}
		} else if a != nil {
			for _, value := range a {
				s := object(value)
				if str(s["pkg"]) == "" || str(s["name"]) == "" || str(s["apk"]) == "" {
					return n, errors.New("invalid legacy extension index")
				}
				n.Items = append(n.Items, model.Item{ID: str(s["pkg"]), Name: str(s["name"]), Language: str(s["lang"]), Group: "extension", Data: raw(s)})
			}
		} else {
			return n, errors.New("expected repo.json metadata or legacy extension index array")
		}
		n.Config = raw(v)
		n.Warnings = append(n.Warnings, "Extension APKs are never downloaded, installed or re-signed by Relay")
	case "podcast":
		if str(o["schema"]) != "shadow.podcast/v1" || str(o["title"]) == "" {
			return n, errors.New("podcast requires schema shadow.podcast/v1 and title")
		}
		if link := str(o["link"]); link != "" {
			if e := security.SafeURL(link); e != nil {
				return n, e
			}
		}
		if logo := str(o["image"]); logo != "" {
			if e := security.SafeURL(logo); e != nil {
				return n, e
			}
		}
		seenIDs, seenURLs := map[string]bool{}, map[string]bool{}
		for _, value := range list(o["episodes"]) {
			s := object(value)
			u := resolve(base, str(s["url"]))
			if str(s["title"]) == "" || !strings.HasPrefix(str(s["type"]), "audio/") {
				return n, errors.New("episodes require title, audio MIME type and URL")
			}
			if e := security.SafeURL(u); e != nil {
				return n, e
			}
			length, ok := s["length"].(float64)
			if !ok || length < 0 || length != float64(int64(length)) {
				return n, errors.New("episode byte length must be a nonnegative integer")
			}
			id := str(s["id"])
			if id == "" {
				id = security.Hash([]byte(u))
			}
			if seenIDs[id] || seenURLs[u] {
				return n, errors.New("podcast episode IDs and audio URLs must be unique")
			}
			seenIDs[id], seenURLs[u] = true, true
			if date := str(s["publishedAt"]); date != "" {
				if _, e := time.Parse(time.RFC3339, date); e != nil {
					return n, errors.New("episode publishedAt must be an RFC3339 timestamp")
				}
			}
			s["url"] = u
			n.Items = append(n.Items, model.Item{ID: id, Name: str(s["title"]), URL: u, MIME: str(s["type"]), Data: raw(s)})
		}
		if len(n.Items) == 0 {
			return n, errors.New("podcast has no episodes")
		}
		n.Config = raw(o)
	default:
		return n, fmt.Errorf("unsupported extra protocol %s", n.Protocol)
	}
	return n, nil
}

// yuanc publishes link (not url), and its directory path carries the category.
func catalogProtocol(base string) string {
	for marker, proto := range map[string]string{"/legado/books": "legado-book", "/legado/rss": "legado-rss", "/legado/tts": "legado-tts", "/legado/purify": "legado-replace", "/ysc/": "tvbox", "/iptv/": "m3u"} {
		if strings.Contains(base, marker) {
			return proto
		}
	}
	return ""
}

func catalogLink(base, link string) string {
	// yuanc resolves "data/..." from the site/repository root, unlike ordinary
	// URL-relative links. Keep raw GitHub's owner/repository/ref prefix intact.
	if strings.HasPrefix(link, "data/") {
		if u, e := url.Parse(base); e == nil {
			if index := strings.Index(u.Path, "/data/"); index >= 0 {
				u.Path = u.Path[:index+1]
				u.RawQuery = ""
				u.RawPath = ""
				return resolve(u.String(), link)
			}
		}
	}
	return resolve(base, link)
}

func scrubSoNovel(v any) {
	if entries, ok := v.([]any); ok {
		for _, entry := range entries {
			scrubSoNovel(entry)
		}
		return
	}
	m, ok := v.(map[string]any)
	if !ok || m["toc"] == nil || m["chapter"] == nil {
		return
	}
	search := object(m["search"])
	if search == nil {
		return
	}
	if value := search["cookies"]; value != nil {
		empty := false
		switch typed := value.(type) {
		case string:
			empty = strings.TrimSpace(typed) == "" || strings.TrimSpace(typed) == "{}"
		case map[string]any:
			empty = len(typed) == 0
		}
		if !empty {
			search["relayRequiresCredentials"] = true
		}
	}
	delete(search, "cookies")
	if value := str(search["url"]); value != "" {
		search["url"] = strings.ReplaceAll(value, "%s", "{keyword}")
	}
}
