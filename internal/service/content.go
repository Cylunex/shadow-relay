package service

import (
	"context"
	"errors"

	"github.com/Cylunex/shadow-relay/internal/adapter"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// StageContent updates a locally managed playlist/rule/feed through the same review
// path as remote updates, preserving the active publication until approval.
func (s *Service) StageContent(ctx context.Context, id, content, expected string) (model.Revision, error) {
	var result model.Revision
	src, e := store.Get[model.Source](ctx, s.DB.Pool, "sources", id)
	if e != nil {
		return result, e
	}
	if src.URL != "" {
		return result, errors.New("remote sources update through their subscription; import a local copy to edit")
	}
	if expected != "" && expected != src.UpdatedAt {
		return result, ErrConflict
	}
	n, e := adapter.Parse([]byte(content), src.Protocol, "")
	if e != nil {
		return result, e
	}
	hash, e := s.Vault.Snapshot([]byte(content))
	if e != nil {
		return result, e
	}
	e = s.DB.Write(ctx, func(tx *store.Tx) error {
		current, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		if current.UpdatedAt != src.UpdatedAt {
			return ErrConflict
		}
		old := model.Revision{}
		if current.ActiveRevision != "" {
			old, e = store.Get[model.Revision](ctx, tx, "revisions", current.ActiveRevision)
			if e != nil {
				return e
			}
		}
		if current.StagedRevision != "" {
			staged, e := store.Get[model.Revision](ctx, tx, "revisions", current.StagedRevision)
			if e != nil {
				return e
			}
			if staged.Hash == hash {
				result = staged
				return nil
			}
		}
		if old.Hash == hash {
			return errors.New("content is identical to the active revision")
		}
		result = model.Revision{ID: model.ID("rev"), SourceID: id, Hash: hash, Normalized: n, Status: "staged", Diff: adapter.Difference(old.Normalized, n), CreatedAt: model.Now()}
		if e = store.Insert(ctx, tx, "revisions", result.ID, result); e != nil {
			return e
		}
		current.StagedRevision = result.ID
		current.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "sources", id, current); e != nil {
			return e
		}
		return audit(ctx, tx, "source.content.stage", id)
	})
	return result, e
}
