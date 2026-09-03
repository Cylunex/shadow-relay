package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// Reduced, non-sensitive reproductions of the publication shapes found on NAS.
// Real packs are exercised in the NAS-only isolated API run and never committed.
func TestLargeLegacyPackDoesNotHitHubPluginLimit(t *testing.T) {
	s := harness(t)
	entries := []any{}
	for i := 0; i < 592; i++ {
		entries = append(entries, map[string]any{"bookSourceName": fmt.Sprintf("Book %d", i), "bookSourceUrl": fmt.Sprintf("https://books.example.com/%d", i), "ruleSearch": map[string]string{"name": "@js:result"}})
	}
	src := imported(t, s, string(jsonBytes(entries)))
	approve(t, s, src)
	set, err := s.SaveSet(t.Context(), "", model.SourceSet{Name: "Reading", Members: []model.Member{{SourceID: src.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Publish(t.Context(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actual []any
	if json.Unmarshal([]byte(p.Artifacts["legado/books.json"].Body), &actual) != nil || len(actual) != 592 {
		t.Fatal("legacy rules were dropped")
	}
	var manifest struct {
		Entries     []any `json:"entries"`
		Unsupported int   `json:"unsupported"`
	}
	if json.Unmarshal([]byte(p.Artifacts["hub/plugins.json"].Body), &manifest) != nil || len(manifest.Entries) != 0 || manifest.Unsupported != 592 {
		t.Fatal("unsupported rules entered bridge manifest")
	}
	if !strings.Contains(p.FormatWarnings["hub/plugins.json"], "592") {
		t.Fatal("missing compatibility warning")
	}
}

func TestBookLanguageFilterIsAppliedToLegacyExport(t *testing.T) {
	s := harness(t)
	src := imported(t, s, `[{"bookSourceName":"Chinese","bookSourceUrl":"https://books.example.com/zh"},{"bookSourceName":"English","bookSourceUrl":"https://books.example.com/en"}]`)
	approve(t, s, src)
	current, _ := store.Get[model.Source](t.Context(), s.DB.Pool, "sources", src.ID)
	err := s.DB.Write(t.Context(), func(tx *store.Tx) error {
		rev, err := store.Get[model.Revision](t.Context(), tx, "revisions", current.ActiveRevision)
		if err != nil {
			return err
		}
		rev.Normalized.Items[0].Language = "zh-CN"
		rev.Normalized.Items[1].Language = "en"
		return store.Put(t.Context(), tx, "revisions", rev.ID, rev)
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := s.SaveSet(t.Context(), "", model.SourceSet{Name: "Reading", Members: []model.Member{{SourceID: src.ID, Languages: []string{"zh-CN"}}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Publish(t.Context(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actual []map[string]any
	json.Unmarshal([]byte(p.Artifacts["legado/books.json"].Body), &actual)
	if len(actual) != 1 || actual[0]["bookSourceName"] != "Chinese" {
		t.Fatal("legacy export ignored member language filter")
	}
}

func TestPublicationPreviewAndValidationPreservePointer(t *testing.T) {
	s := harness(t)
	src := imported(t, s, `[{"bookSourceName":"Book","bookSourceUrl":"https://books.example.com"}]`)
	set, err := s.SaveSet(t.Context(), "", model.SourceSet{Name: "Reading", Members: []model.Member{{SourceID: src.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PreviewPublication(t.Context(), set.ID)
	var failure *PublicationError
	if !errors.As(err, &failure) || failure.Exclusions[src.ID] != "disabled" {
		t.Fatal("missing exclusion diagnostics", err)
	}
	approve(t, s, src)
	preview, err := s.PreviewPublication(t.Context(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get[model.Publication](t.Context(), s.DB.Pool, "publications", preview.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("preview was persisted")
	}
	unchanged, _ := store.Get[model.SourceSet](t.Context(), s.DB.Pool, "source_sets", set.ID)
	if unchanged.CurrentPublication != "" {
		t.Fatal("preview moved subscription")
	}
	first, err := s.Publish(t.Context(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := store.Get[model.Source](t.Context(), s.DB.Pool, "sources", src.ID)
	// Simulate a historic normalized record that fails today's publication validation.
	err = s.DB.Write(t.Context(), func(tx *store.Tx) error {
		rev, err := store.Get[model.Revision](t.Context(), tx, "revisions", current.ActiveRevision)
		if err != nil {
			return err
		}
		rev.Normalized.Items[0].Data = json.RawMessage(`{"bookSourceName":"Book","bookSourceUrl":"https://reader:private@books.example.com/"}`)
		return store.Put(t.Context(), tx, "revisions", rev.ID, rev)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Publish(t.Context(), set.ID)
	if !errors.As(err, &failure) || failure.SourceErrors[src.ID] == "" || strings.Contains(failure.SourceErrors[src.ID], "private") {
		t.Fatal("missing or unsafe source validation diagnostics", err)
	}
	unchanged, _ = store.Get[model.SourceSet](t.Context(), s.DB.Pool, "source_sets", set.ID)
	if unchanged.CurrentPublication != first.ID {
		t.Fatal("failed publish moved subscription")
	}
}
