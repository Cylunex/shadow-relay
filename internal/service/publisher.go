package service

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

var Formats = []string{"shadow.json", "tvbox/store.json", "iptv/live.m3u", "iptv/epg.xml", "legado/books.json", "legado/rss.json", "legado/tts.json", "feeds.opml", "opds/"}

const BasePlaceholder = "__RELAY_PUBLICATION_BASE__"

func (s *Service) SaveSet(ctx context.Context, id string, set model.SourceSet) (model.SourceSet, error) {
	if e := requireName(set.Name); e != nil {
		return set, e
	}
	if len(set.Description) > 2000 || len(set.Members) > 500 {
		return set, errors.New("source set exceeds limit")
	}
	set.ID = id
	if id == "" {
		set.ID = model.ID("set")
	}
	set.CurrentPublication = ""
	set.PreviousPublication = ""
	set.UpdatedAt = model.Now()
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if id != "" {
			old, e := store.Get[model.SourceSet](ctx, tx, "source_sets", id)
			if e != nil {
				return e
			}
			set.CurrentPublication = old.CurrentPublication
			set.PreviousPublication = old.PreviousPublication
		}
		seen := map[string]bool{}
		for i := range set.Members {
			m := &set.Members[i]
			if seen[m.SourceID] {
				return errors.New("duplicate source in set")
			}
			seen[m.SourceID] = true
			if _, e := store.Get[model.Source](ctx, tx, "sources", m.SourceID); e != nil {
				return e
			}
			if m.Role == "" {
				m.Role = "primary"
			}
			if !slices.Contains([]string{"primary", "backup", "auxiliary"}, m.Role) || m.MinScore < 0 || m.MinScore > 100 || m.Priority < 0 || m.Priority > 10000 || m.Weight < 0 || m.Weight > 10000 {
				return errors.New("invalid member role, priority, weight or score")
			}
			if m.Weight == 0 {
				m.Weight = 1
			}
			if m.TimeoutMS == 0 {
				m.TimeoutMS = 15000
			}
			if m.MaxConcurrency == 0 {
				m.MaxConcurrency = 2
			}
			if m.TimeoutMS < 100 || m.TimeoutMS > 120000 || m.MaxConcurrency < 1 || m.MaxConcurrency > 32 {
				return errors.New("invalid member timeout or concurrency")
			}
			for _, mt := range m.MediaTypes {
				if !slices.Contains(mediaTypes, mt) {
					return errors.New("invalid media filter")
				}
			}
		}
		if e := store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
			return e
		}
		return audit(ctx, tx, "source_set.save", set.ID)
	})
	return set, e
}

type selected struct {
	Source   model.Source
	Revision model.Revision
	Member   model.Member
	Endpoint string
	Driver   string
}

func (s *Service) Publish(ctx context.Context, id string) (model.Publication, error) {
	var pub model.Publication
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		set, e := store.Get[model.SourceSet](ctx, tx, "source_sets", id)
		if e != nil {
			return e
		}
		members := slices.Clone(set.Members)
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].Priority != members[j].Priority {
				return members[i].Priority > members[j].Priority
			}
			if members[i].Role != members[j].Role {
				return roleOrder(members[i].Role) < roleOrder(members[j].Role)
			}
			return members[i].SourceID < members[j].SourceID
		})
		items := []selected{}
		excluded := map[string]string{}
		for _, m := range members {
			src, e := store.Get[model.Source](ctx, tx, "sources", m.SourceID)
			if e != nil {
				return e
			}
			reason := ""
			switch {
			case !src.Enabled:
				reason = "disabled"
			case src.ActiveRevision == "":
				reason = "no_approved_revision"
			case src.Health == "quarantined" || src.Health == "failing" || src.Health == "disabled":
				reason = src.Health
			case src.Score < m.MinScore:
				reason = "below_minimum_health"
			case src.Mode == "catalog-only":
				reason = "catalog_only"
			case len(m.MediaTypes) > 0 && !overlap(src.MediaTypes, m.MediaTypes):
				reason = "media_filtered"
			}
			if reason != "" {
				excluded[src.ID] = reason
				continue
			}
			r, e := store.Get[model.Revision](ctx, tx, "revisions", src.ActiveRevision)
			if e != nil {
				return e
			}
			if r.Status != "approved" {
				return errors.New("active revision is not approved")
			}
			if (len(m.Languages) > 0 || len(m.Regions) > 0) && len(filtered(r.Normalized.Items, m)) == 0 {
				excluded[src.ID] = "item_filter_empty"
				continue
			}
			endpoint, driver := src.URL, src.Protocol
			if src.RuntimeID != "" {
				rt, e := store.Get[model.Runtime](ctx, tx, "runtimes", src.RuntimeID)
				if e != nil {
					return e
				}
				if rt.Health != "healthy" {
					excluded[src.ID] = "runtime_unavailable"
					continue
				}
				endpoint = rt.URL
				driver = rt.Driver
			}
			items = append(items, selected{src, r, m, endpoint, driver})
		}
		if len(items) == 0 {
			return errors.New("no eligible sources; current publication is preserved")
		}
		pub, e = Compile(set, items, excluded)
		if e != nil {
			return e
		}
		if e = store.Insert(ctx, tx, "publications", pub.ID, pub); e != nil {
			return e
		}
		set.PreviousPublication = set.CurrentPublication
		set.CurrentPublication = pub.ID
		set.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "source_sets", id, set); e != nil {
			return e
		}
		return audit(ctx, tx, "publication.publish", pub.ID)
	})
	return pub, e
}
func roleOrder(r string) int {
	switch r {
	case "primary":
		return 0
	case "backup":
		return 1
	default:
		return 2
	}
}
func overlap(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}
func filtered(items []model.Item, m model.Member) []model.Item {
	out := []model.Item{}
	for _, x := range items {
		if len(m.Languages) > 0 && !slices.Contains(m.Languages, x.Language) {
			continue
		}
		if len(m.Regions) > 0 && !slices.Contains(m.Regions, x.Region) {
			continue
		}
		out = append(out, x)
	}
	return out
}
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func attr(s string) string { return strings.NewReplacer("\"", "'", "\n", " ", "\r", " ").Replace(s) }
func Compile(set model.SourceSet, items []selected, excluded map[string]string) (model.Publication, error) {
	p := model.Publication{ID: model.ID("pub"), SetID: set.ID, SourceRevisions: map[string]string{}, Exclusions: excluded, Artifacts: map[string]model.Artifact{}, CreatedAt: model.Now()}
	providers := []any{}
	channels, feeds, books := []model.Item{}, []model.Item{}, []model.Item{}
	tvStores := []any{}
	legado := map[string][]any{"legado-book": {}, "legado-rss": {}, "legado-tts": {}}
	xmlDocs := []string{}
	seen := map[string]bool{}
	add := func(path, typ, body string) {
		p.Artifacts[path] = model.Artifact{ContentType: typ, Body: body, Hash: security.Hash([]byte(body))}
	}
	for _, v := range items {
		src, n, m := v.Source, v.Revision.Normalized, v.Member
		p.SourceRevisions[src.ID] = v.Revision.ID
		media := src.MediaTypes
		if len(m.MediaTypes) > 0 {
			media = []string{}
			for _, x := range src.MediaTypes {
				if slices.Contains(m.MediaTypes, x) {
					media = append(media, x)
				}
			}
		}
		endpoint := v.Endpoint
		its := filtered(n.Items, m)
		if src.Mode == "compiled" && src.Protocol != "shadow-bundle" {
			path, typ, body, err := sourceArtifact(src, n, its, p.CreatedAt)
			if err != nil {
				return p, err
			}
			if path != "" {
				add(path, typ, body)
				endpoint = BasePlaceholder + "/" + path
			}
		}
		driver := v.Driver
		if src.Mode == "compiled" && src.Protocol == "opds2" {
			driver = "opds1"
		}
		provider := map[string]any{"id": src.ID, "name": src.Name, "mediaTypes": media, "driver": driver, "mode": src.Mode, "endpoint": endpoint, "capabilities": src.Capabilities, "priority": m.Priority, "weight": m.Weight, "role": m.Role, "health": src.Health, "score": src.Score, "credentialMode": "client-local", "revision": v.Revision.ID, "constraints": map[string]any{"devices": m.Devices, "networks": m.Networks, "timeoutMs": m.TimeoutMS, "maxConcurrency": m.MaxConcurrency}}
		if src.Protocol == "shadow-bundle" {
			for _, item := range n.Items {
				var imported map[string]any
				if e := json.Unmarshal(item.Data, &imported); e != nil {
					return p, e
				}
				imported["id"] = src.ID + ":" + item.ID
				imported["priority"] = m.Priority
				imported["role"] = m.Role
				imported["health"] = src.Health
				imported["score"] = src.Score
				imported["credentialMode"] = "client-local"
				imported["constraints"] = provider["constraints"]
				providers = append(providers, imported)
			}
		} else if !n.RequiresRuntime || src.RuntimeID != "" {
			providers = append(providers, provider)
		}
		// Device/network restrictions are client selection hints in Bundle. Legacy formats cannot represent them and exclude such members.
		if len(m.Devices) > 0 || len(m.Networks) > 0 {
			continue
		}
		switch src.Protocol {
		case "m3u":
			for _, x := range its {
				key := "channel:" + x.URL
				if !seen[key] {
					channels = append(channels, x)
					seen[key] = true
				}
			}
		case "xmltv":
			var doc string
			if e := json.Unmarshal(n.Config, &doc); e != nil {
				return p, e
			}
			xmlDocs = append(xmlDocs, doc)
		case "tvbox":
			config := map[string]any{"sites": []any{}, "urls": []any{}}
			if e := json.Unmarshal(n.Config, &config); e != nil {
				return p, e
			}
			if sites, ok := config["sites"].([]any); ok && len(sites) > 0 {
				add("tvbox/"+src.ID+".json", "application/json", string(jsonBytes(map[string]any{"sites": sites})))
				tvStores = append(tvStores, map[string]string{"name": src.Name, "url": BasePlaceholder + "/tvbox/" + src.ID + ".json"})
			}
			if urls, ok := config["urls"].([]any); ok {
				for _, u := range urls {
					tvStores = append(tvStores, u)
				}
			}
		case "legado-book", "legado-rss", "legado-tts":
			var entries []any
			if e := json.Unmarshal(n.Config, &entries); e != nil {
				return p, e
			}
			for _, x := range entries {
				key := src.Protocol + security.Hash(jsonBytes(x))
				if !seen[key] {
					legado[src.Protocol] = append(legado[src.Protocol], x)
					seen[key] = true
				}
			}
		case "opml":
			feeds = append(feeds, its...)
		case "rss", "atom", "json-feed":
			if src.URL != "" {
				feeds = append(feeds, model.Item{Name: src.Name, URL: src.URL})
			}
			var body string
			typ := "application/xml"
			if src.Protocol == "json-feed" {
				body = string(n.Config)
				typ = "application/feed+json"
			} else {
				_ = json.Unmarshal(n.Config, &body)
			}
			if body != "" {
				add("sources/"+src.ID, typ, body)
			}
		case "opds1", "opds2":
			books = append(books, its...)
		}
	}
	if len(channels) > 0 {
		add("iptv/live.m3u", "audio/x-mpegurl", playlistBody(channels))
	}
	if len(xmlDocs) > 0 {
		body, e := mergeXMLTV(xmlDocs)
		if e != nil {
			return p, e
		}
		add("iptv/epg.xml", "application/xml", body)
	}
	if len(tvStores) > 0 {
		add("tvbox/store.json", "application/json", string(jsonBytes(map[string]any{"urls": tvStores})))
	}
	for proto, path := range map[string]string{"legado-book": "legado/books.json", "legado-rss": "legado/rss.json", "legado-tts": "legado/tts.json"} {
		if len(legado[proto]) > 0 {
			add(path, "application/json", string(jsonBytes(legado[proto])))
		}
	}
	if len(feeds) > 0 {
		add("feeds.opml", "text/x-opml", opmlBody(set.Name, feeds))
	}
	if len(books) > 0 {
		add("opds/", "application/atom+xml;profile=opds-catalog", opdsBody(set.ID, set.Name, p.CreatedAt, books))
	}
	if len(providers) == 0 && len(p.Artifacts) == 0 {
		return p, errors.New("publication has no usable providers or exports")
	}
	exports := map[string]string{}
	for _, f := range Formats {
		if _, ok := p.Artifacts[f]; ok {
			exports[f] = BasePlaceholder + "/" + f
		}
	}
	p.Revision = "sha256:" + security.Hash(jsonBytes(map[string]any{"providers": providers, "artifacts": p.Artifacts, "set": set.ID}))
	bundle := map[string]any{"schema": "shadow.media.bundle/v1", "bundleId": set.ID, "name": set.Name, "publicationId": p.ID, "revision": p.Revision, "generatedAt": p.CreatedAt, "providers": providers, "exports": exports}
	add("shadow.json", "application/json", string(jsonBytes(bundle)))
	size := 0
	for _, a := range p.Artifacts {
		size += len(a.Body)
		if e := security.ValidateDocument([]byte(a.Body)); e != nil {
			return p, e
		}
	}
	if size > 32<<20 {
		return p, errors.New("compiled publication exceeds 32 MiB")
	}
	return p, nil
}
func mergeXMLTV(docs []string) (string, error) {
	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?><tv>`)
	enc := xml.NewEncoder(&out)
	seen := map[string]bool{}
	for _, doc := range docs {
		d := xml.NewDecoder(strings.NewReader(doc))
		for {
			t, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", e
			}
			start, ok := t.(xml.StartElement)
			if !ok || (start.Name.Local != "channel" && start.Name.Local != "programme") {
				continue
			}
			key := start.Name.Local
			for _, a := range start.Attr {
				if a.Name.Local == "id" || a.Name.Local == "channel" || a.Name.Local == "start" {
					key += "|" + a.Name.Local + "=" + a.Value
				}
			}
			if seen[key] {
				if e = d.Skip(); e != nil {
					return "", e
				}
				continue
			}
			seen[key] = true
			if e = enc.EncodeToken(start); e != nil {
				return "", e
			}
			depth := 1
			for depth > 0 {
				t, e = d.Token()
				if e != nil {
					return "", e
				}
				switch t.(type) {
				case xml.StartElement:
					depth++
				case xml.EndElement:
					depth--
				}
				if e = enc.EncodeToken(t); e != nil {
					return "", e
				}
			}
		}
	}
	if e := enc.Flush(); e != nil {
		return "", e
	}
	out.WriteString("</tv>")
	return out.String(), nil
}
func (s *Service) RollbackPublication(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		p, e := store.Get[model.Publication](ctx, tx, "publications", id)
		if e != nil {
			return e
		}
		set, e := store.Get[model.SourceSet](ctx, tx, "source_sets", p.SetID)
		if e != nil {
			return e
		}
		if set.CurrentPublication == id {
			return nil
		}
		set.PreviousPublication = set.CurrentPublication
		set.CurrentPublication = id
		set.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "source_sets", set.ID, set); e != nil {
			return e
		}
		return audit(ctx, tx, "publication.rollback", id)
	})
}
func (s *Service) CreateBinding(ctx context.Context, b model.Binding) (model.Binding, string, error) {
	if e := requireName(b.Name); e != nil {
		return b, "", e
	}
	if len(b.Formats) == 0 {
		b.Formats = slices.Clone(Formats)
	}
	for _, f := range b.Formats {
		if !slices.Contains(Formats, f) {
			return b, "", errors.New("unsupported binding format")
		}
	}
	expiry, e := time.Parse(time.RFC3339, b.ExpiresAt)
	if e != nil || !expiry.After(time.Now()) {
		return b, "", errors.New("binding expiry must be a future RFC3339 timestamp")
	}
	token := security.Token()
	b.ID = model.ID("binding")
	b.Hash = security.Hash([]byte(token))
	b.CreatedAt = model.Now()
	b.Generation = 1
	b.Revoked = false
	e = s.DB.Write(ctx, func(tx *store.Tx) error {
		if _, e := store.Get[model.SourceSet](ctx, tx, "source_sets", b.SetID); e != nil {
			return e
		}
		if e = store.Insert(ctx, tx, "bindings", b.ID, b); e != nil {
			return e
		}
		return audit(ctx, tx, "binding.create", b.ID)
	})
	b.Hash = ""
	return b, token, e
}
func (s *Service) BindingAction(ctx context.Context, id, action string) (string, error) {
	token := ""
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		b, e := store.Get[model.Binding](ctx, tx, "bindings", id)
		if e != nil {
			return e
		}
		switch action {
		case "revoke":
			b.Revoked = true
		case "rotate":
			expiry, e := time.Parse(time.RFC3339, b.ExpiresAt)
			if e != nil || !expiry.After(time.Now()) {
				return errors.New("expired binding cannot rotate; create a new binding")
			}
			token = security.Token()
			b.Hash = security.Hash([]byte(token))
			b.Generation++
			b.Revoked = false
		default:
			return errors.New("unknown binding action")
		}
		if e = store.Put(ctx, tx, "bindings", id, b); e != nil {
			return e
		}
		return audit(ctx, tx, "binding."+action, id)
	})
	return token, e
}
func (s *Service) Resolve(ctx context.Context, token, publication, path string) (model.Artifact, error) {
	if len(token) != 43 {
		return model.Artifact{}, store.ErrNotFound
	}
	var id string
	e := s.DB.Pool.QueryRow(ctx, "SELECT id FROM bindings WHERE token_hash=$1", security.Hash([]byte(token))).Scan(&id)
	if e != nil {
		return model.Artifact{}, store.ErrNotFound
	}
	b, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", id)
	if e != nil {
		return model.Artifact{}, e
	}
	expiry, e := time.Parse(time.RFC3339, b.ExpiresAt)
	if e != nil || b.Revoked || !expiry.After(time.Now()) {
		return model.Artifact{}, store.ErrNotFound
	}
	allowed := slices.Contains(b.Formats, path)
	if strings.HasPrefix(path, "tvbox/") && slices.Contains(b.Formats, "tvbox/store.json") {
		allowed = true
	}
	if strings.HasPrefix(path, "sources/") && slices.Contains(b.Formats, "shadow.json") {
		allowed = true
	}
	if !allowed {
		return model.Artifact{}, store.ErrNotFound
	}
	if publication == "" {
		set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", b.SetID)
		if e != nil {
			return model.Artifact{}, e
		}
		publication = set.CurrentPublication
	}
	p, e := store.Get[model.Publication](ctx, s.DB.Pool, "publications", publication)
	if e != nil || p.SetID != b.SetID {
		return model.Artifact{}, store.ErrNotFound
	}
	a, ok := p.Artifacts[path]
	if !ok {
		return a, store.ErrNotFound
	}
	return a, nil
}

func (s *Service) Feedback(ctx context.Context, token string, in model.Feedback) error {
	if !slices.Contains([]string{"timeout", "unavailable", "parse_error", "unauthorized", "unsupported"}, in.Code) {
		return errors.New("unknown feedback error class")
	}
	if _, e := s.Resolve(ctx, token, in.PublicationID, "shadow.json"); e != nil {
		return e
	}
	var bindingID string
	if e := s.DB.Pool.QueryRow(ctx, "SELECT id FROM bindings WHERE token_hash=$1", security.Hash([]byte(token))).Scan(&bindingID); e != nil {
		return store.ErrNotFound
	}
	b, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", bindingID)
	if e != nil {
		return e
	}
	if in.PublicationID == "" {
		set, e := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", b.SetID)
		if e != nil {
			return e
		}
		in.PublicationID = set.CurrentPublication
	}
	p, e := store.Get[model.Publication](ctx, s.DB.Pool, "publications", in.PublicationID)
	if e != nil {
		return e
	}
	if _, ok := p.SourceRevisions[in.SourceID]; !ok {
		parent, _, found := strings.Cut(in.SourceID, ":")
		if _, allowed := p.SourceRevisions[parent]; !found || !allowed {
			return store.ErrNotFound
		}
		in.SourceID = parent
	}
	in.BindingID = bindingID
	in.CreatedAt = model.Now()
	in.ID = "feedback_" + security.Hash([]byte(bindingID + in.SourceID + in.Code + time.Now().UTC().Format("200601021504")))[:24]
	// A client report is a hint, never an authority to quarantine a source. Deduplicate within each minute.
	return s.DB.Write(ctx, func(tx *store.Tx) error { return store.Put(ctx, tx, "feedback", in.ID, in) })
}

func sourceArtifact(src model.Source, n model.Normalized, items []model.Item, created string) (path, typ, body string, err error) {
	path = "sources/" + src.ID
	switch src.Protocol {
	case "m3u":
		path += "/live.m3u"
		typ = "audio/x-mpegurl"
		body = playlistBody(items)
	case "opml":
		path += "/feeds.opml"
		typ = "text/x-opml"
		body = opmlBody(src.Name, items)
	case "opds1", "opds2":
		path += "/opds.xml"
		typ = "application/atom+xml;profile=opds-catalog"
		body = opdsBody(src.ID, src.Name, created, items)
	case "tvbox":
		path += "/tvbox.json"
		typ = "application/json"
		body = string(n.Config)
	case "xmltv", "rss", "atom":
		typ = "application/xml"
		err = json.Unmarshal(n.Config, &body)
	case "json-feed":
		typ = "application/feed+json"
		body = string(n.Config)
	default:
		path = ""
	}
	return
}
func playlistBody(items []model.Item) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	seen := map[string]bool{}
	for _, c := range items {
		if seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n", attr(c.ID), attr(c.Logo), attr(c.Group), attr(c.Name), c.URL)
	}
	return b.String()
}
func opmlBody(name string, items []model.Item) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head><title>` + xmlEscape(name) + `</title></head><body>`)
	seen := map[string]bool{}
	for _, x := range items {
		if x.URL == "" || seen[x.URL] {
			continue
		}
		seen[x.URL] = true
		fmt.Fprintf(&b, `<outline type="rss" text="%s" xmlUrl="%s"/>`, xmlEscape(x.Name), xmlEscape(x.URL))
	}
	b.WriteString("</body></opml>")
	return b.String()
}
func opdsBody(id, name, created string, items []model.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><id>urn:shadow-relay:%s</id><title>%s</title><updated>%s</updated>`, xmlEscape(id), xmlEscape(name), created)
	seen := map[string]bool{}
	for _, x := range items {
		if x.URL == "" || seen[x.URL] {
			continue
		}
		seen[x.URL] = true
		rel := x.Rel
		if rel == "" {
			rel = "subsection"
		}
		fmt.Fprintf(&b, `<entry><id>urn:sha256:%s</id><title>%s</title><updated>%s</updated><link rel="%s" href="%s" type="%s"/></entry>`, security.Hash([]byte(x.URL)), xmlEscape(x.Name), created, xmlEscape(rel), xmlEscape(x.URL), xmlEscape(x.MIME))
	}
	b.WriteString("</feed>")
	return b.String()
}
