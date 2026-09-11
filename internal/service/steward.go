package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

// ItemIdentity builds a stable preference/dedupe key: protocol|entry|lang|account.
func ItemIdentity(protocol, entryID, entryURL, lang, account string) string {
	entry := strings.TrimSpace(entryID)
	if entry == "" {
		entry = strings.TrimSpace(entryURL)
	}
	parts := []string{strings.ToLower(strings.TrimSpace(protocol)), entry, strings.ToLower(strings.TrimSpace(lang)), strings.TrimSpace(account)}
	return strings.Join(parts, "|")
}

func preferenceID(setID, itemKey string) string {
	return "pref_" + security.Hash([]byte(setID+"\n"+itemKey))[:24]
}

func (s *Service) SavePreference(ctx context.Context, in model.PreferenceOverlay) (model.PreferenceOverlay, error) {
	if in.SetID == "" || in.ItemKey == "" {
		return in, errors.New("setId and itemKey required")
	}
	in.ID = preferenceID(in.SetID, in.ItemKey)
	in.UpdatedAt = model.Now()
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if _, e := store.Get[model.SourceSet](ctx, tx, "source_sets", in.SetID); e != nil {
			return e
		}
		if e := store.Put(ctx, tx, "preferences", in.ID, in); e != nil {
			return e
		}
		return audit(ctx, tx, "preference.save", in.ID)
	})
	return in, e
}

func (s *Service) ListPreferences(ctx context.Context, setID string) ([]model.PreferenceOverlay, error) {
	all, e := store.List[model.PreferenceOverlay](ctx, s.DB.Pool, "preferences")
	if e != nil {
		return nil, e
	}
	if setID == "" {
		return all, nil
	}
	out := []model.PreferenceOverlay{}
	for _, p := range all {
		if p.SetID == setID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Service) DeletePreference(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		if e := store.Delete(ctx, tx, "preferences", id); e != nil {
			return e
		}
		return audit(ctx, tx, "preference.delete", id)
	})
}

// SoftDeleteSource stops serving a source but keeps revisions for undo.
// Manual SourceSet membership must still be cleared first (same as hard delete).
func (s *Service) SoftDeleteSource(ctx context.Context, id string) error {
	var proto string
	var media []string
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		src, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		proto, media = src.Protocol, src.MediaTypes
		if e = stripAggregateMembershipTx(ctx, tx, id); e != nil {
			return e
		}
		sets, e := store.List[model.SourceSet](ctx, tx, "source_sets")
		if e != nil {
			return e
		}
		for _, set := range sets {
			if IsAggregateSetID(set.ID) {
				continue
			}
			for _, m := range set.Members {
				if m.SourceID == id {
					return errors.New("remove source from its source sets before deletion")
				}
			}
		}
		src.Enabled = false
		src.Health = "disabled"
		src.UpdatedAt = model.Now()
		archived := model.DeletedSource{ID: id, Source: src, DeletedAt: model.Now()}
		if e = store.Put(ctx, tx, "deleted_sources", id, archived); e != nil {
			return e
		}
		if e = store.Put(ctx, tx, "sources", id, src); e != nil {
			return e
		}
		return audit(ctx, tx, "source.soft_delete", id)
	})
	if e != nil {
		return e
	}
	return s.ensureAggregatePublished(ctx, AggregateTypeFor(proto, media))
}

func (s *Service) UndoDeleteSource(ctx context.Context, id string) (model.Source, error) {
	var out model.Source
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if _, e := store.Get[model.DeletedSource](ctx, tx, "deleted_sources", id); e != nil {
			return e
		}
		src, e := store.Get[model.Source](ctx, tx, "sources", id)
		if e != nil {
			return e
		}
		if e = store.Delete(ctx, tx, "deleted_sources", id); e != nil {
			return e
		}
		src.UpdatedAt = model.Now()
		if e = store.Put(ctx, tx, "sources", id, src); e != nil {
			return e
		}
		out = src
		return audit(ctx, tx, "source.undo_delete", id)
	})
	return out, e
}

// IsSoftDeleted reports whether a source is in the undo tombstone table.
func (s *Service) IsSoftDeleted(ctx context.Context, id string) bool {
	_, e := store.Get[model.DeletedSource](ctx, s.DB.Pool, "deleted_sources", id)
	return e == nil
}

// ConfirmMassDelete approves a staged revision that was held for mass-delete risk.
func (s *Service) ConfirmMassDelete(ctx context.Context, id, revision string) error {
	return s.SourceAction(ctx, id, "approve-enable", revision)
}

// RecoverSyncLock removes a stale Hub plugin sync lock older than maxAge.
func RecoverSyncLock(root string, maxAge time.Duration) (bool, error) {
	lock := filepath.Join(root, ".relay-sync-lock")
	info, err := os.Stat(lock)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if time.Since(info.ModTime()) < maxAge {
		return false, errors.New("another sync is active or its lock needs recovery")
	}
	if err = os.RemoveAll(lock); err != nil {
		return false, err
	}
	return true, nil
}

const personalCredentialID = "personal_client"
const personalCredentialOwner = "personal_client_token"

// EnsurePersonalCredential creates or returns the long-lived personal subscribe entry
// covering all aggregate bindings (one connect, per-type formats via aggregates).
func (s *Service) EnsurePersonalCredential(ctx context.Context, publicBase string) (map[string]any, error) {
	infos, e := s.ListAggregates(ctx, publicBase)
	if e != nil {
		return nil, e
	}
	// Ensure every aggregate bucket has a binding so personal entry can advertise them.
	for _, def := range aggregateBuckets {
		_ = s.DB.Write(ctx, func(tx *store.Tx) error {
			if _, e := s.ensureAggregateSetTx(ctx, tx, def); e != nil {
				return e
			}
			return s.ensureAggregateBindingTx(ctx, tx, def.Slug)
		})
	}
	infos, e = s.ListAggregates(ctx, publicBase)
	if e != nil {
		return nil, e
	}
	entries := []map[string]any{}
	for _, info := range infos {
		if info.Token == "" {
			continue
		}
		entries = append(entries, map[string]any{
			"type": info.Type, "setId": info.SetID, "name": info.Name,
			"token": info.Token, "subscribeUrls": info.SubscribeURLs,
			"memberCount": info.MemberCount, "formats": Formats,
		})
	}
	return map[string]any{
		"id": personalCredentialID, "name": "个人媒体订阅",
		"entries": entries, "note": "One personal connect: use the aggregate URL matching the client (Hub books, TVBox/M3U live, lx-music descriptor, etc.). Reset rotates all aggregate tokens.",
	}, nil
}

// ResetPersonalCredential rotates every aggregate binding token (one-click reset).
func (s *Service) ResetPersonalCredential(ctx context.Context, publicBase string) (map[string]any, error) {
	for _, def := range aggregateBuckets {
		bID := aggregateBindingID(def.Slug)
		b, e := store.Get[model.Binding](ctx, s.DB.Pool, "bindings", bID)
		if errors.Is(e, store.ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if b.Revoked {
			continue
		}
		if _, e = s.BindingAction(ctx, bID, "rotate"); e != nil {
			return nil, fmt.Errorf("rotate %s: %w", def.Slug, e)
		}
	}
	return s.EnsurePersonalCredential(ctx, publicBase)
}

// FilterProvidersByDrivers drops Bundle providers whose driver is unsupported by the client.
// Unsupported items become unavailable; the feed itself still succeeds.
func FilterProvidersByDrivers(bundle map[string]any, allowed []string) map[string]any {
	if len(allowed) == 0 {
		return bundle
	}
	allow := map[string]bool{}
	for _, d := range allowed {
		allow[strings.ToLower(d)] = true
	}
	providers, _ := bundle["providers"].([]any)
	kept := []any{}
	skipped := 0
	for _, p := range providers {
		m, _ := p.(map[string]any)
		driver, _ := m["driver"].(string)
		if allow[strings.ToLower(driver)] {
			kept = append(kept, p)
		} else {
			skipped++
		}
	}
	bundle["providers"] = kept
	if skipped > 0 {
		warnings, _ := bundle["formatWarnings"].(map[string]any)
		if warnings == nil {
			warnings = map[string]any{}
		}
		warnings["providers"] = fmt.Sprintf("%d providers unavailable for this client capability filter", skipped)
		bundle["formatWarnings"] = warnings
	}
	return bundle
}
