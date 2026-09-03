package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func bookReport(src model.Source, n model.Normalized) bookplugin.Report {
	entries := []json.RawMessage{}
	for _, item := range n.Items {
		entries = append(entries, item.Data)
	}
	report := bookplugin.Convert(src.Protocol, entries, src.ID)
	for _, entry := range report.Entries {
		if entry.Recipe != nil && src.HubProxyMode != "" {
			entry.Recipe.ProxyMode = src.HubProxyMode
		}
	}
	return report
}
func (s *Service) BookReport(ctx context.Context, id, revision string) (bookplugin.Report, string, error) {
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", id)
	if e != nil {
		return bookplugin.Report{}, "", e
	}
	if revision == "" {
		revision = src.StagedRevision
		if revision == "" {
			revision = src.ActiveRevision
		}
	}
	r, e := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", revision)
	if e != nil {
		return bookplugin.Report{}, "", e
	}
	if r.SourceID != id {
		return bookplugin.Report{}, "", store.ErrNotFound
	}
	return bookReport(src, r.Normalized), r.CreatedAt, nil
}

func extendedArtifacts(setID, setName, generatedAt string, items []selected, warnings map[string]string, add func(string, string, string)) error {
	report := bookplugin.Report{SetID: setID, GeneratedAt: generatedAt, Schema: "shadow.hub.plugins/v1", Entries: []bookplugin.Entry{}}
	repos := []any{}
	podcastItems := []model.Item{}
	seenAudio := map[string]bool{}
	for _, v := range items {
		if len(v.Member.Devices) > 0 || len(v.Member.Networks) > 0 {
			continue
		}
		n, src := v.Revision.Normalized, v.Source
		switch src.Protocol {
		case "podcast":
			for _, episode := range filtered(n.Items, v.Member) {
				if !seenAudio[episode.URL] {
					episode.ID = src.ID + ":" + episode.ID
					podcastItems = append(podcastItems, episode)
					seenAudio[episode.URL] = true
				}
			}
		case "legado-book", "so-novel", "relay-book":
			n.Items = filtered(n.Items, v.Member)
			r := bookReport(src, n)
			for _, entry := range r.Entries {
				if entry.Recipe != nil && len(entry.Blockers) == 0 {
					report.Entries = append(report.Entries, entry)
				}
			}
			report.Supported += r.Supported
			report.Unsupported += r.Unsupported
		case "mihon-repo":
			var meta map[string]any
			_ = json.Unmarshal(n.Config, &meta)
			index := ""
			if meta != nil {
				index, _ = meta["index_v2"].(string)
			}
			repos = append(repos, map[string]any{"sourceId": src.ID, "name": src.Name, "repositoryUrl": src.URL, "indexV2": index, "metadata": meta["meta"], "entryCount": len(n.Items)})
		}
	}
	if len(report.Entries) > 500 {
		return errors.New("Hub manifest exceeds 500 rules; select a smaller source pack or split this set")
	}
	if report.Unsupported > 0 {
		warnings["hub/plugins.json"] = fmt.Sprintf("%d 条规则不能转换为 Hub 插件；完整原因请查看源的书源兼容报告。阅读格式独立保留原规则。", report.Unsupported)
	}
	if report.Supported+report.Unsupported > 0 {
		add("hub/plugins.json", "application/json", string(jsonBytes(report)))
	}
	if len(repos) > 0 {
		add("mihon/repos.json", "application/json", string(jsonBytes(map[string]any{"schema": "shadow.mihon.repositories/v1", "repositories": repos, "note": "Register the original upstream URLs in Mihon; this catalog is not a merged or re-signed extension repository"})))
	}
	if len(podcastItems) > 0 {
		add("podcasts/feed.xml", "application/rss+xml", podcastBody(model.Normalized{Items: podcastItems, Config: jsonBytes(map[string]string{"title": setName, "description": "Published with Shadow Relay", "link": BasePlaceholder + "/podcasts/feed.xml"})}))
	}
	return nil
}

func txtBody(items []model.Item) string {
	groups := []string{}
	byGroup := map[string][]model.Item{}
	for _, item := range items {
		group := strings.NewReplacer(",", "，", "\n", " ", "\r", " ").Replace(item.Group)
		if group == "" {
			group = "未分组"
		}
		if _, ok := byGroup[group]; !ok {
			groups = append(groups, group)
		}
		byGroup[group] = append(byGroup[group], item)
	}
	var out strings.Builder
	for _, group := range groups {
		fmt.Fprintf(&out, "%s,#genre#\n", group)
		for _, item := range byGroup[group] {
			fmt.Fprintf(&out, "%s,%s\n", strings.ReplaceAll(item.Name, ",", "，"), item.URL)
		}
	}
	return out.String()
}
func podcastBody(n model.Normalized, fallbackLink ...string) string {
	var c map[string]any
	_ = json.Unmarshal(n.Config, &c)
	if c == nil {
		c = map[string]any{}
	}
	if link, _ := c["link"].(string); link == "" && len(fallbackLink) > 0 {
		c["link"] = fallbackLink[0]
	}
	str := func(k string) string { v, _ := c[k].(string); return xmlEscape(v) }
	var out strings.Builder
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>%s</title><link>%s</link><description>%s</description>`, str("title"), str("link"), str("description"))
	if str("image") != "" {
		fmt.Fprintf(&out, "<image><url>%s</url><title>%s</title><link>%s</link></image>", str("image"), str("title"), str("link"))
	}
	for _, item := range n.Items {
		var d struct {
			Description string `json:"description"`
			PublishedAt string `json:"publishedAt"`
			Length      int64  `json:"length"`
		}
		_ = json.Unmarshal(item.Data, &d)
		fmt.Fprintf(&out, `<item><guid isPermaLink="false">%s</guid><title>%s</title><description>%s</description><enclosure url="%s" type="%s" length="%d"/>`, xmlEscape(item.ID), xmlEscape(item.Name), xmlEscape(d.Description), xmlEscape(item.URL), xmlEscape(item.MIME), d.Length)
		if t, e := time.Parse(time.RFC3339, d.PublishedAt); e == nil {
			fmt.Fprintf(&out, "<pubDate>%s</pubDate>", t.Format(time.RFC1123Z))
		}
		out.WriteString("</item>")
	}
	out.WriteString("</channel></rss>")
	return out.String()
}

func validateChannelRules(rules []model.ChannelRule) error {
	if len(rules) > 2000 {
		return errors.New("maximum 2000 channel overrides")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.Match == "" || len(r.Name+r.Group+r.TVGID) > 1000 || strings.ContainsAny(r.Name+r.Group+r.TVGID, "\r\n\x00") {
			return errors.New("invalid channel override")
		}
		key := r.SourceID + "|" + r.Match
		if seen[key] {
			return errors.New("duplicate channel override")
		}
		seen[key] = true
		if r.Logo != "" {
			if e := security.SafeURL(r.Logo); e != nil {
				return e
			}
		}
	}
	return nil
}
func channelOverrides(items []model.Item, sourceID string, rules []model.ChannelRule) []model.Item {
	out := []model.Item{}
	for _, item := range items {
		hidden := false
		originalID := item.ID
		for _, r := range rules {
			if r.SourceID != "" && r.SourceID != sourceID {
				continue
			}
			if r.Match != item.URL && r.Match != originalID {
				continue
			}
			hidden = r.Hide
			if r.Name != "" {
				item.Name = r.Name
			}
			if r.Group != "" {
				item.Group = r.Group
			}
			if r.Logo != "" {
				item.Logo = r.Logo
			}
			if r.TVGID != "" {
				item.ID = r.TVGID
			}
		}
		if !hidden {
			out = append(out, item)
		}
	}
	return out
}

func ConvertBookPreview(n model.Normalized, hubProxyMode string) bookplugin.Report {
	return bookReport(model.Source{Protocol: n.Protocol, HubProxyMode: hubProxyMode}, n)
}
func ScaffoldBook(r bookplugin.Recipe) ([]byte, error) {
	report := bookplugin.Convert("relay-book", []json.RawMessage{jsonBytes(r)}, "")
	return bookplugin.Archive(report, model.Now())
}

func publicationInputs(items []selected) []any {
	out := []any{}
	for _, v := range items {
		out = append(out, map[string]any{"id": v.Source.ID, "name": v.Source.Name, "revision": v.Revision.ID, "mode": v.Source.Mode, "mediaTypes": v.Source.MediaTypes, "health": v.Source.Health, "score": v.Source.Score, "endpoint": v.Endpoint, "driver": v.Driver, "hubProxyMode": v.Source.HubProxyMode})
	}
	return out
}
