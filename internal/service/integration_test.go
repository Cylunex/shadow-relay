package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/Cylunex/shadow-relay/internal/testutil"
)

type fakeFetch struct {
	result fetch.Result
	err    error
	mu     sync.Mutex
	calls  int
	fn     func(string) (fetch.Result, error)
}

func (f *fakeFetch) Get(_ context.Context, u string, _ fetch.Policy, _ map[string]string, _ int64, _ bool) (fetch.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fn != nil {
		return f.fn(u)
	}
	return f.result, f.err
}
func harness(t *testing.T) *Service {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	v, e := security.NewVault(base64.StdEncoding.EncodeToString(key), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return &Service{DB: testutil.Database(t), Vault: v, Fetch: &fakeFetch{result: fetch.Result{Status: 200, Body: []byte("media bytes"), ContentType: "video/mp2t"}}}
}

const playlist = "#EXTM3U\n#EXTINF:-1 tvg-id=\"one\",One\nhttps://media.example.com/one.ts\n#EXTINF:-1 tvg-id=\"two\",Two\nhttps://media.example.com/two.ts"

func imported(t *testing.T, s *Service, body string) model.Source {
	t.Helper()
	src, e := s.Import(context.Background(), Input{Name: "Example source", Content: body})
	if e != nil {
		t.Fatal(e)
	}
	return src
}
func approve(t *testing.T, s *Service, src model.Source) {
	t.Helper()
	for _, a := range []string{"approve", "enable"} {
		if e := s.SourceAction(context.Background(), src.ID, a, ""); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPublishBindingAndRollbackLifecycle(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	if src.Enabled || src.ActiveRevision != "" {
		t.Fatal("import bypassed review")
	}
	if e := s.SourceAction(ctx, src.ID, "enable", ""); e == nil {
		t.Fatal("enabled unreviewed source")
	}
	approve(t, s, src)
	set, e := s.SaveSet(ctx, "", model.SourceSet{Name: "Home", Members: []model.Member{{SourceID: src.ID, Priority: 100, MinScore: 50}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Publish(ctx, set.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(p.Artifacts["iptv/live.m3u"].Body, "One") {
		t.Fatal("playlist missing")
	}
	b, token, e := s.CreateBinding(ctx, model.Binding{Name: "TV", SetID: set.ID, Formats: []string{"iptv/live.m3u"}, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, token, "", "iptv/live.m3u"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, token, "", "shadow.json"); e == nil {
		t.Fatal("binding scope bypassed")
	}
	p2, e := s.Publish(ctx, set.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RollbackPublication(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	current, _ := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", set.ID)
	if current.CurrentPublication != p.ID || current.PreviousPublication != p2.ID {
		t.Fatal("rollback not atomic")
	}
	token2, e := s.BindingAction(ctx, b.ID, "rotate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, token, "", "iptv/live.m3u"); e == nil {
		t.Fatal("old token remains active")
	}
	if _, e = s.Resolve(ctx, token2, p.ID, "iptv/live.m3u"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BindingAction(ctx, b.ID, "revoke"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, token2, p.ID, "iptv/live.m3u"); e == nil {
		t.Fatal("revocation failed")
	}
}
func TestQuarantinePreservesLastKnownGoodAndPublicationIsolation(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	approve(t, s, src)
	set, e := s.SaveSet(ctx, "", model.SourceSet{Name: "Home", Members: []model.Member{{SourceID: src.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Publish(ctx, set.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.Fetch = &fakeFetch{err: errors.New("offline")}
	for i := 0; i < 3; i++ {
		_ = s.Probe(ctx, src.ID)
	}
	v, _ := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if v.Health != "quarantined" {
		t.Fatalf("expected quarantine: %+v", v)
	}
	if _, e = s.Publish(ctx, set.ID); e == nil {
		t.Fatal("empty publication replaced good one")
	}
	current, _ := store.Get[model.SourceSet](ctx, s.DB.Pool, "source_sets", set.ID)
	if current.CurrentPublication != p.ID {
		t.Fatal("last known good lost")
	}
	other, _ := s.SaveSet(ctx, "", model.SourceSet{Name: "Other"})
	_, token, e := s.CreateBinding(ctx, model.Binding{Name: "Other client", SetID: other.ID, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, token, p.ID, "shadow.json"); e == nil {
		t.Fatal("cross-set publication leak")
	}
}
func TestAutoUpdateRequiresFunctionalSampleAndReviewForRiskyDiff(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src, e := s.Import(ctx, Input{Name: "Live", Content: playlist, URL: "https://media.example.com/list.m3u", UpdatePolicy: "auto"})
	if e != nil {
		t.Fatal(e)
	}
	approve(t, s, src)
	old, _ := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	newBody := playlist + "\n#EXTINF:-1 tvg-id=\"three\",Three\nhttps://media.example.com/three.ts"
	s.Fetch = &fakeFetch{fn: func(u string) (fetch.Result, error) {
		if strings.HasSuffix(u, "list.m3u") {
			return fetch.Result{Status: 200, Body: []byte(newBody)}, nil
		}
		return fetch.Result{}, errors.New("sample unavailable")
	}}
	if e = s.SyncSource(ctx, src.ID); e != nil {
		t.Fatal(e)
	}
	v, _ := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if v.ActiveRevision != old.ActiveRevision || v.StagedRevision == "" {
		t.Fatal("failed sample auto-approved")
	}
	if e = s.SourceAction(ctx, src.ID, "reject", ""); e != nil {
		t.Fatal(e)
	}
	newBody += "\n#EXTINF:-1 tvg-id=\"four\",Four\nhttps://media.example.com/four.ts"
	s.Fetch = &fakeFetch{fn: func(u string) (fetch.Result, error) {
		if strings.HasSuffix(u, "list.m3u") {
			return fetch.Result{Status: 200, Body: []byte(newBody)}, nil
		}
		return fetch.Result{Status: 200, Body: []byte("stream"), ContentType: "video/mp2t"}, nil
	}}
	if e = s.SyncSource(ctx, src.ID); e != nil {
		t.Fatal(e)
	}
	v, _ = store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if v.ActiveRevision == old.ActiveRevision {
		t.Fatal("healthy low-risk update was not approved")
	}
	good := v.ActiveRevision
	newBody = "#EXTM3U\n#EXTINF:-1 tvg-id=\"one\",One\nhttps://other.example.com/one.ts"
	if e = s.SyncSource(ctx, src.ID); e != nil {
		t.Fatal(e)
	}
	v, _ = store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if v.ActiveRevision != good || v.StagedRevision == "" {
		t.Fatal("risky update was not staged")
	}
	rev, _ := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", v.StagedRevision)
	if !rev.Diff.RequiresReview {
		t.Fatal("missing risk marker")
	}
}
func TestCatalogIsIdempotentAndNeverAutoEnables(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	cat, e := s.SaveCatalog(ctx, "", model.Catalog{Name: "Catalog", URL: "https://catalog.example.com/list.json", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	s.Fetch = &fakeFetch{result: fetch.Result{Status: 200, Body: []byte(`[{"name":"Live","url":"https://media.example.com/list.m3u"}]`)}}
	for i := 0; i < 2; i++ {
		if e = s.SyncCatalog(ctx, cat.ID); e != nil {
			t.Fatal(e)
		}
	}
	cs, _ := store.List[model.Candidate](ctx, s.DB.Pool, "candidates")
	ss, _ := store.List[model.Source](ctx, s.DB.Pool, "sources")
	if len(cs) != 1 || len(ss) != 0 {
		t.Fatal("catalog duplicated or auto-imported")
	}
	s.Fetch = &fakeFetch{result: fetch.Result{Status: 200, Body: []byte(playlist)}}
	if e = s.CandidateAction(ctx, cs[0].ID, "accept"); e != nil {
		t.Fatal(e)
	}
	if e = s.CandidateAction(ctx, cs[0].ID, "accept"); e == nil {
		t.Fatal("candidate accepted twice")
	}
	ss, _ = store.List[model.Source](ctx, s.DB.Pool, "sources")
	if len(ss) != 1 || ss[0].Enabled || ss[0].ActiveRevision != "" {
		t.Fatal("candidate bypassed source approval")
	}
}
func TestQueueConcurrentClaimAndWorkerRestartLease(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	approve(t, s, src)
	for i := 0; i < 2; i++ {
		if _, e := s.Enqueue(ctx, "source.probe", src.ID); e != nil {
			t.Fatal(e)
		}
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 1 {
		t.Fatal("dedup failed", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.work(ctx) }()
	}
	wg.Wait()
	jobs, _ = s.Jobs(ctx)
	if jobs[0].Status != "succeeded" || jobs[0].Attempts != 1 {
		t.Fatalf("job claimed more than once: %+v", jobs)
	}
	id, e := s.Enqueue(ctx, "source.probe", src.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Pool.Exec(ctx, `UPDATE jobs SET status='running',attempts=1,lease_until=now()-interval '1 minute',lease_token='expired' WHERE id=$1`, id)
	if e != nil {
		t.Fatal(e)
	}
	if !s.work(ctx) {
		t.Fatal("expired lease not reclaimed")
	}
	jobs, _ = s.Jobs(ctx)
	for _, j := range jobs {
		if j.ID == id && (j.Status != "succeeded" || j.Attempts != 2) {
			t.Fatal("lease recovery failed")
		}
	}
}
func TestCredentialsNotInBundleAndInvalidRuntimeAPI(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src, e := s.Import(ctx, Input{Name: "Media", Protocol: "emby", URL: "https://media.example.com", Headers: map[string]string{"X-Emby-Token": "never-publish-this"}})
	if e != nil {
		t.Fatal(e)
	}
	approve(t, s, src)
	set, e := s.SaveSet(ctx, "", model.SourceSet{Name: "Home", Members: []model.Member{{SourceID: src.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Publish(ctx, set.ID)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(jsonBytes(p)), "never-publish-this") {
		t.Fatal("upstream secret leaked")
	}
	secret, _ := store.Get[model.Secret](ctx, s.DB.Pool, "secrets", src.ID)
	if strings.Contains(secret.Ciphertext, "never-publish-this") {
		t.Fatal("secret was not encrypted")
	}
	rt, e := s.SaveRuntime(ctx, "", model.Runtime{Name: "ABS", Driver: "audiobookshelf", URL: "https://audio.example.com"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	s.Fetch = &fakeFetch{result: fetch.Result{Status: 200, Body: []byte(`<html>Login</html>`)}}
	if e = s.TestRuntime(ctx, rt.ID, false); e == nil {
		t.Fatal("HTML login page marked healthy")
	}
	var n map[string]any
	if json.Unmarshal([]byte(p.Artifacts["shadow.json"].Body), &n) != nil {
		t.Fatal("invalid bundle")
	}
}
func TestCompilerDedupOrderingEscapingAndConstraints(t *testing.T) {
	n := model.Normalized{Protocol: "m3u", Items: []model.Item{{ID: "one", Name: "One & Two", URL: "https://media.example.com/a", Group: "News"}, {ID: "dup", Name: "Duplicate", URL: "https://media.example.com/a"}}}
	src := model.Source{ID: "a", Protocol: "m3u", Mode: "compiled", MediaTypes: []string{"video.live"}}
	p, e := Compile(model.SourceSet{ID: "home", Name: "Home"}, []selected{{Source: src, Revision: model.Revision{ID: "r", Normalized: n}, Member: model.Member{Role: "primary"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(p.Artifacts["iptv/live.m3u"].Body, "#EXTINF") != 1 {
		t.Fatal("playlist not deduplicated")
	}
	p, e = Compile(model.SourceSet{ID: "home"}, []selected{{Source: src, Revision: model.Revision{ID: "r", Normalized: n}, Member: model.Member{Devices: []string{"tv"}}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := p.Artifacts["iptv/live.m3u"]; ok {
		t.Fatal("legacy format ignored device constraint")
	}
	xml, e := mergeXMLTV([]string{`<tv><channel id="one"><display-name>A &amp; B</display-name></channel></tv>`, `<tv><channel id="one"><display-name>Duplicate</display-name></channel></tv>`})
	if e != nil || strings.Count(xml, "<channel") != 1 || !strings.Contains(xml, "A &amp; B") {
		t.Fatal("XMLTV merge failed", xml, e)
	}
}

func TestConstrainedBundleKeepsItsOwnSnapshot(t *testing.T) {
	n := model.Normalized{Protocol: "m3u", Items: []model.Item{{Name: "Only TV", URL: "https://media.example.com/tv.ts"}}}
	src := model.Source{ID: "src_tv", Protocol: "m3u", Mode: "compiled", MediaTypes: []string{"video.live"}}
	p, e := Compile(model.SourceSet{ID: "home"}, []selected{{Source: src, Driver: "m3u", Revision: model.Revision{ID: "r", Normalized: n}, Member: model.Member{Devices: []string{"tv"}, Weight: 1}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := p.Artifacts["iptv/live.m3u"]; ok {
		t.Fatal("restricted member escaped into legacy aggregate")
	}
	if !strings.Contains(p.Artifacts["sources/src_tv/live.m3u"].Body, "Only TV") {
		t.Fatal("bundle lost its source snapshot")
	}
	if !strings.Contains(p.Artifacts["shadow.json"].Body, "sources/src_tv/live.m3u") {
		t.Fatal("provider points to the wrong artifact")
	}
}
func TestLegacyRulesExportWithoutClaimingAProvider(t *testing.T) {
	s := harness(t)
	src := imported(t, s, `[{"bookSourceName":"Example book","bookSourceUrl":"https://books.example.com","ruleSearch":{"name":"a@text"}}]`)
	approve(t, s, src)
	set, e := s.SaveSet(context.Background(), "", model.SourceSet{Name: "Reading", Members: []model.Member{{SourceID: src.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Publish(context.Background(), set.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := p.Artifacts["legado/books.json"]; !ok {
		t.Fatal("legacy export missing")
	}
	var b struct {
		Providers []any `json:"providers"`
	}
	if e = json.Unmarshal([]byte(p.Artifacts["shadow.json"].Body), &b); e != nil || len(b.Providers) != 0 {
		t.Fatal("rules advertised as callable provider")
	}
}
