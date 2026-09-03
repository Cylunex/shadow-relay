package service

import (
	"context"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func TestApproveEnableRequiresCurrentStagedRevision(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	other := imported(t, s, playlist)
	for _, revision := range []string{"", "rev_stale", other.StagedRevision} {
		if err := s.SourceAction(ctx, src.ID, "approve-enable", revision); err == nil {
			t.Fatal("approved missing, stale or unrelated revision", revision)
		}
	}
	for _, status := range []string{"invalid", "rejected", "approved"} {
		if err := s.DB.Write(ctx, func(tx *store.Tx) error {
			r, err := store.Get[model.Revision](ctx, tx, "revisions", src.StagedRevision)
			if err != nil {
				return err
			}
			r.Status = status
			return store.Put(ctx, tx, "revisions", r.ID, r)
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.SourceAction(ctx, src.ID, "approve-enable", src.StagedRevision); err == nil {
			t.Fatal("approved non-staged revision", status)
		}
	}
	got, err := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if err != nil || got.Enabled || got.ActiveRevision != "" || got.StagedRevision != src.StagedRevision {
		t.Fatal("failed approval changed source", got, err)
	}
	var audits int
	if err := s.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM audits WHERE data->>'action' IN ('source.approve','source.enable')").Scan(&audits); err != nil || audits != 0 {
		t.Fatal("failed approval wrote audit", audits, err)
	}
}

func TestApproveEnableMakesImportedSourcePublishable(t *testing.T) {
	s := harness(t)
	ctx := context.Background()
	src := imported(t, s, playlist)
	if err := s.SourceAction(ctx, src.ID, "disable", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SourceAction(ctx, src.ID, "approve-enable", src.StagedRevision); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get[model.Source](ctx, s.DB.Pool, "sources", src.ID)
	if err != nil || !got.Enabled || got.ActiveRevision != src.StagedRevision || got.StagedRevision != "" || got.Health != "degraded" || got.Score != 60 {
		t.Fatal("approval did not enable source", got, err)
	}
	rev, err := store.Get[model.Revision](ctx, s.DB.Pool, "revisions", got.ActiveRevision)
	if err != nil || rev.Status != "approved" {
		t.Fatal("revision not approved", rev, err)
	}
	var audits int
	if err := s.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM audits WHERE data->>'action' IN ('source.approve','source.enable')").Scan(&audits); err != nil || audits != 2 {
		t.Fatal("missing approval/enable audit", audits, err)
	}
	set, err := s.SaveSet(ctx, "", model.SourceSet{Name: "Approved imports", Members: []model.Member{{SourceID: src.ID, MinScore: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := s.Publish(ctx, set.ID)
	if err != nil || pub.SourceRevisions[src.ID] != got.ActiveRevision {
		t.Fatal("approved import not publishable", pub.Exclusions, err)
	}
}
