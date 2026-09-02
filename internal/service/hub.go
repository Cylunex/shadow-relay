package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

type runtimeWriter interface {
	PostJSON(context.Context, string, fetch.Policy, map[string]string, []byte, int64) (fetch.Result, error)
}

func (s *Service) hubJSON(ctx context.Context, rt model.Runtime, path string, body any) (map[string]any, error) {
	h, e := s.Headers(ctx, rt.ID)
	if e != nil {
		return nil, e
	}
	u := strings.TrimRight(rt.URL, "/") + "/api/console/" + path
	policy := fetch.Policy{Network: rt.Network, Trust: rt.Trust}
	var res fetch.Result
	if body == nil {
		res, e = s.Fetch.Get(ctx, u, policy, h, 4<<20, false)
	} else {
		writer, ok := s.Fetch.(runtimeWriter)
		if !ok {
			return nil, errors.New("runtime write transport unavailable")
		}
		res, e = writer.PostJSON(ctx, u, policy, h, jsonBytes(body), 4<<20)
	}
	if e != nil {
		return nil, e
	}
	var result map[string]any
	if json.Unmarshal(res.Body, &result) != nil {
		return nil, errors.New("incompatible Hub API response")
	}
	return result, nil
}

// ProbeHub uses the real Hub console search job and candidate verification APIs.
// No content or upstream credentials are persisted in Relay's probe history.
func (s *Service) ProbeHub(ctx context.Context, src model.Source, n model.Normalized) (model.Probe, error) {
	start := time.Now()
	p := model.Probe{ID: model.ID("probe"), SourceID: src.ID, Level: "functional", Code: "hub_live_smoke_failed", Checks: []string{}, CreatedAt: model.Now()}
	finish := func(e error) (model.Probe, error) {
		p.Success = e == nil
		p.LatencyMS = time.Since(start).Milliseconds()
		if e == nil {
			p.Code = "hub_live_smoke_passed"
		}
		return p, e
	}
	rt, e := store.Get[model.Runtime](ctx, s.DB.Pool, "runtimes", src.RuntimeID)
	if e != nil {
		return finish(e)
	}
	if rt.Driver != "legado-hub" {
		return finish(errors.New("live book smoke requires LegadoHub"))
	}
	ids := []string{}
	if src.HubPluginID != "" {
		ids = append(ids, src.HubPluginID)
	} else {
		report := bookReport(src, n)
		for _, entry := range report.Entries {
			if entry.Recipe != nil {
				ids = append(ids, entry.ID)
			}
		}
	}
	if len(ids) == 0 {
		return finish(errors.New("no convertible Hub plugin; configure an existing Hub plugin ID or convert the source"))
	}
	// Rotating sampling covers a pack over successive checks without starting hundreds of searches.
	index := 0
	if len(ids) > 1 {
		index = int(time.Now().Unix()/int64(max(15, src.ProbeIntervalMinutes)*60)) % len(ids)
	}
	pluginID := ids[index]
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,96}$`).MatchString(pluginID) {
		return finish(errors.New("invalid plugin identifier"))
	}
	if src.HubPluginID == "" {
		report := bookReport(src, n)
		expected := ""
		for _, entry := range report.Entries {
			if entry.ID == pluginID && entry.Recipe != nil {
				expected = bookplugin.Version(*entry.Recipe)
			}
		}
		installed, e := s.hubJSON(ctx, rt, "plugins/"+pluginID, nil)
		if e != nil {
			return finish(e)
		}
		if installed["version"] != expected {
			return finish(errors.New("Hub has not loaded this rule revision; sync plugins before live smoke"))
		}
	}
	keyword := src.SmokeKeyword
	if keyword == "" {
		return finish(errors.New("configure a live smoke keyword first"))
	}
	job, e := s.hubJSON(ctx, rt, "search-jobs", map[string]any{"keyword": keyword, "page": 1, "sourceIds": []string{pluginID}, "limit": 1})
	if e != nil {
		return finish(e)
	}
	jobID, _ := job["jobId"].(string)
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,120}$`).MatchString(jobID) {
		return finish(errors.New("Hub did not return a search job"))
	}
	var snapshot map[string]any
	for attempt := 0; attempt < 20; attempt++ {
		snapshot, e = s.hubJSON(ctx, rt, "search-jobs/"+url.PathEscape(jobID), nil)
		if e != nil {
			return finish(e)
		}
		status, _ := snapshot["status"].(string)
		if status != "running" && status != "pending" {
			break
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return finish(ctx.Err())
		case <-timer.C:
		}
	}
	var candidates []any
	if items, ok := snapshot["items"].([]any); ok {
		candidates = items
	}
	if result, ok := snapshot["result"].(map[string]any); ok {
		if items, ok := result["items"].([]any); ok {
			candidates = items
		}
	}
	candidateID := ""
	for _, value := range candidates {
		item, _ := value.(map[string]any)
		if item["sourceId"] == pluginID {
			candidateID, _ = item["candidateId"].(string)
			if candidateID != "" {
				break
			}
		}
	}
	if candidateID == "" {
		groups, e := s.hubJSON(ctx, rt, "search-jobs/"+url.PathEscape(jobID)+"/candidates", nil)
		if e != nil {
			return finish(e)
		}
		gs, _ := groups["items"].([]any)
		for _, g := range gs {
			group, _ := g.(map[string]any)
			items, _ := group["items"].([]any)
			for _, v := range items {
				item, _ := v.(map[string]any)
				if item["sourceId"] == pluginID {
					candidateID, _ = item["candidateId"].(string)
					if candidateID != "" {
						break
					}
				}
			}
			if candidateID != "" {
				break
			}
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,120}$`).MatchString(candidateID) {
		return finish(errors.New("live search found no verifiable candidate"))
	}
	p.Checks = append(p.Checks, "hub_search_result", "plugin:"+pluginID)
	verified, e := s.hubJSON(ctx, rt, "search-jobs/"+url.PathEscape(jobID)+"/candidates/"+url.PathEscape(candidateID)+"/verify", map[string]any{"chapterIndex": 0, "includeReviews": false})
	if e != nil {
		return finish(e)
	}
	result, _ := verified["result"].(map[string]any)
	if result["pluginId"] != pluginID || result["passed"] != true {
		return finish(errors.New("Hub content verification did not pass"))
	}
	for _, stage := range []string{"detail", "toc", "chapter"} {
		v, _ := result[stage].(map[string]any)
		if v["passed"] != true {
			return finish(errors.New("Hub returned an incomplete verification"))
		}
		p.Checks = append(p.Checks, "hub_"+stage)
	}
	toc, _ := result["toc"].(map[string]any)
	chapters, _ := toc["items"].([]any)
	if len(chapters) == 0 {
		return finish(errors.New("Hub did not provide a directory for completeness checks"))
	}
	seen := map[string]bool{}
	lastTitle := ""
	for i, v := range chapters {
		ch, _ := v.(map[string]any)
		u, _ := ch["chapterUrl"].(string)
		index, _ := ch["index"].(float64)
		if u == "" || seen[u] || int(index) != i+1 {
			return finish(errors.New("directory contains duplicate URLs or nonsequential indexes"))
		}
		seen[u] = true
		lastTitle, _ = ch["title"].(string)
	}
	detail, _ := result["detail"].(map[string]any)
	latest, _ := detail["lastChapter"].(string)
	if latest != "" && normalizeTitle(latest) != normalizeTitle(lastTitle) {
		return finish(errors.New("directory tail does not match the detail page latest chapter"))
	}
	p.Checks = append(p.Checks, "unique_sequential_directory")
	if latest != "" {
		p.Checks = append(p.Checks, "directory_tail_matches_latest")
	} else {
		p.Checks = append(p.Checks, "directory_completeness_needs_fixture")
	}
	return finish(nil)
}
func normalizeTitle(v string) string { return strings.Join(strings.Fields(v), "") }

func (s *Service) HubPlugins(ctx context.Context, id string) (any, error) {
	rt, e := store.Get[model.Runtime](ctx, s.DB.Pool, "runtimes", id)
	if e != nil {
		return nil, e
	}
	if rt.Driver != "legado-hub" {
		return nil, errors.New("runtime is not LegadoHub")
	}
	v, e := s.hubJSON(ctx, rt, "plugins", nil)
	if e != nil {
		return nil, e
	}
	return v, nil
}
func (s *Service) ReloadHub(ctx context.Context, id string) error {
	rt, e := store.Get[model.Runtime](ctx, s.DB.Pool, "runtimes", id)
	if e != nil {
		return e
	}
	if rt.Driver != "legado-hub" {
		return errors.New("runtime is not LegadoHub")
	}
	v, e := s.hubJSON(ctx, rt, "plugins/reload", map[string]any{})
	if e != nil {
		return e
	}
	if v["reloaded"] != true {
		return errors.New("Hub did not confirm reload")
	}
	return s.DB.Write(ctx, func(tx *store.Tx) error { return audit(ctx, tx, "runtime.hub.reload", id) })
}
