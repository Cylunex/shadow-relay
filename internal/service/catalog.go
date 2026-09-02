package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/Cylunex/shadow-relay/internal/adapter"
	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func (s *Service) SaveCatalog(ctx context.Context, id string, c model.Catalog) (model.Catalog, error) {
	in := Input{Name: c.Name, URL: c.URL, Network: c.Network, Trust: c.Trust, IntervalMinutes: c.IntervalMinutes}
	if e := defaults(&in); e != nil {
		return c, e
	}
	if in.URL == "" {
		return c, errors.New("catalog URL required")
	}
	c = model.Catalog{ID: id, Name: in.Name, URL: in.URL, Network: in.Network, Trust: in.Trust, IntervalMinutes: in.IntervalMinutes, Enabled: c.Enabled}
	if id == "" {
		c.ID = model.ID("catalog")
	}
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if id != "" {
			if _, e := store.Get[model.Catalog](ctx, tx, "catalogs", id); e != nil {
				return e
			}
		}
		if e := store.Put(ctx, tx, "catalogs", c.ID, c); e != nil {
			return e
		}
		return audit(ctx, tx, "catalog.save", c.ID)
	})
	return c, e
}
func (s *Service) SyncCatalog(ctx context.Context, id string) error {
	cat, e := store.Get[model.Catalog](ctx, s.DB.Pool, "catalogs", id)
	if e != nil {
		return e
	}
	headers := map[string]string{}
	if cat.ETag != "" {
		headers["If-None-Match"] = cat.ETag
	}
	if cat.LastModified != "" {
		headers["If-Modified-Since"] = cat.LastModified
	}
	res, e := s.Fetch.Get(ctx, cat.URL, fetch.Policy{Network: cat.Network, Trust: cat.Trust}, headers, 8<<20, false)
	if e != nil {
		return e
	}
	var n model.Normalized
	if res.Status != 304 {
		n, e = adapter.Parse(res.Body, "", cat.URL)
		if e != nil {
			return e
		}
		if !slices.Contains([]string{"catalog", "opml", "tvbox"}, n.Protocol) {
			return errors.New("catalog requires name/url JSON entries, OPML or TVBox store")
		}
		if _, e = s.Vault.Snapshot(res.Body); e != nil {
			return e
		}
	}
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		current, e := store.Get[model.Catalog](ctx, tx, "catalogs", id)
		if e != nil {
			return e
		}
		if current.URL != cat.URL {
			return ErrConflict
		}
		entries, e := store.List[model.Candidate](ctx, tx, "candidates")
		if e != nil {
			return e
		}
		known := map[string]bool{}
		for _, c := range entries {
			if c.CatalogID == id {
				known[c.Fingerprint] = true
			}
		}
		for _, item := range n.Items {
			if n.Protocol == "tvbox" && item.Group != "store" {
				continue
			}
			fp := security.Hash([]byte(item.URL))
			if known[fp] {
				continue
			}
			protocol := ""
			if n.Protocol == "tvbox" {
				protocol = "tvbox"
			}
			var meta map[string]string
			if json.Unmarshal(item.Data, &meta) == nil && meta["protocol"] != "" {
				protocol = meta["protocol"]
			}
			c := model.Candidate{ID: model.ID("candidate"), CatalogID: id, Name: item.Name, URL: item.URL, Protocol: protocol, Fingerprint: fp, Status: "pending", DiscoveredAt: model.Now()}
			if e = store.Insert(ctx, tx, "candidates", c.ID, c); e != nil {
				return e
			}
			known[fp] = true
		}
		current.LastSync = model.Now()
		current.NextSync = time.Now().Add(time.Duration(current.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
		if res.Status != 304 || res.ETag != "" {
			current.ETag = res.ETag
		}
		if res.Status != 304 || res.LastModified != "" {
			current.LastModified = res.LastModified
		}
		if e = store.Put(ctx, tx, "catalogs", id, current); e != nil {
			return e
		}
		return audit(ctx, tx, "catalog.sync", id)
	})
}
func (s *Service) CandidateAction(ctx context.Context, id, action string) error {
	c, e := store.Get[model.Candidate](ctx, s.DB.Pool, "candidates", id)
	if e != nil {
		return e
	}
	if action == "accept" {
		if c.Status == "blocked" || c.Status == "accepted" {
			return errors.New("candidate cannot be accepted in current state")
		}
		cat, e := store.Get[model.Catalog](ctx, s.DB.Pool, "catalogs", c.CatalogID)
		if e != nil {
			return e
		}
		in := Input{Name: c.Name, URL: c.URL, Protocol: c.Protocol, Network: cat.Network, Trust: cat.Trust}
		if e = defaults(&in); e != nil {
			return e
		}
		n, b, e := s.Preview(ctx, in)
		if e != nil {
			return e
		}
		hash, e := s.Vault.Snapshot(b)
		if e != nil {
			return e
		}
		src := newSource(in, n)
		src.CatalogID = cat.ID
		rev := model.Revision{ID: model.ID("rev"), SourceID: src.ID, Hash: hash, Normalized: n, Status: "staged", CreatedAt: model.Now(), Diff: adapter.Difference(model.Normalized{}, n)}
		src.StagedRevision = rev.ID
		return s.DB.Write(ctx, func(tx *store.Tx) error {
			v, e := store.Get[model.Candidate](ctx, tx, "candidates", id)
			if e != nil {
				return e
			}
			if v.Status != c.Status {
				return ErrConflict
			}
			if e = store.Insert(ctx, tx, "sources", src.ID, src); e != nil {
				return e
			}
			if e = store.Insert(ctx, tx, "revisions", rev.ID, rev); e != nil {
				return e
			}
			ep := model.Endpoint{ID: src.ID + "_primary", SourceID: src.ID, Role: "primary", URL: src.URL}
			if e = store.Insert(ctx, tx, "endpoints", ep.ID, ep); e != nil {
				return e
			}
			v.Status = "accepted"
			v.SourceID = src.ID
			if e = store.Put(ctx, tx, "candidates", id, v); e != nil {
				return e
			}
			return audit(ctx, tx, "candidate.accept", id)
		})
	}
	if !slices.Contains([]string{"ignore", "block", "reset"}, action) {
		return errors.New("unknown candidate action")
	}
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		v, e := store.Get[model.Candidate](ctx, tx, "candidates", id)
		if e != nil {
			return e
		}
		if v.Status == "accepted" {
			return errors.New("accepted candidate is immutable")
		}
		v.Status = map[string]string{"ignore": "ignored", "block": "blocked", "reset": "pending"}[action]
		if e = store.Put(ctx, tx, "candidates", id, v); e != nil {
			return e
		}
		return audit(ctx, tx, "candidate."+action, id)
	})
}
