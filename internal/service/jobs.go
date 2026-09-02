package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Enqueue(ctx context.Context, kind, target string) (string, error) {
	id := model.ID("job")
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if e := enqueue(ctx, tx, id, kind, target); e != nil {
			return e
		}
		return tx.QueryRow(ctx, "SELECT id FROM jobs WHERE kind=$1 AND target_id=$2 AND status IN ('queued','running')", kind, target).Scan(&id)
	})
	return id, e
}
func enqueue(ctx context.Context, tx *store.Tx, id, kind, target string) error {
	table := ""
	switch kind {
	case "source.sync", "source.probe":
		table = "sources"
	case "catalog.sync":
		table = "catalogs"
	case "runtime.test", "runtime.sync":
		table = "runtimes"
	default:
		return errors.New("unsupported job kind")
	}
	if _, e := store.Get[map[string]any](ctx, tx, table, target); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO jobs(id,kind,target_id) VALUES($1,$2,$3) ON CONFLICT(kind,target_id) WHERE status IN ('queued','running') DO NOTHING`, id, kind, target)
	return e
}
func (s *Service) Jobs(ctx context.Context) ([]model.Job, error) {
	rows, e := s.DB.Pool.Query(ctx, `SELECT id,kind,target_id,status,attempts,error,created_at,finished_at FROM jobs ORDER BY created_at DESC LIMIT 200`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Job{}
	for rows.Next() {
		var j model.Job
		var created time.Time
		var finished *time.Time
		if e = rows.Scan(&j.ID, &j.Kind, &j.TargetID, &j.Status, &j.Attempts, &j.Error, &created, &finished); e != nil {
			return nil, e
		}
		j.CreatedAt = created.UTC().Format(time.RFC3339)
		if finished != nil {
			j.FinishedAt = finished.UTC().Format(time.RFC3339)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s *Service) RetryJob(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE jobs SET status='queued',attempts=0,error='',available_at=now(),finished_at=NULL WHERE id=$1 AND status='failed'`, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return errors.New("only failed jobs can be retried")
		}
		return audit(ctx, tx, "job.retry", id)
	})
}
func (s *Service) Run(ctx context.Context, workers int) {
	var wg sync.WaitGroup
	wg.Add(workers + 1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			_ = s.schedule(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				worked := s.work(ctx)
				if !worked {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
				}
			}
		}()
	}
	wg.Wait()
}
func (s *Service) schedule(ctx context.Context) error {
	return s.DB.Write(ctx, func(tx *store.Tx) error {
		sources, e := store.List[model.Source](ctx, tx, "sources")
		if e != nil {
			return e
		}
		for _, src := range sources {
			if !src.Enabled || src.URL == "" || src.UpdatePolicy == "pinned" || src.UpdatePolicy == "manual" {
				continue
			}
			due, _ := time.Parse(time.RFC3339, src.NextSync)
			if due.After(time.Now()) {
				continue
			}
			if e = enqueue(ctx, tx, model.ID("job"), "source.sync", src.ID); e != nil {
				return e
			}
			src.NextSync = time.Now().Add(time.Duration(src.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
			if e = store.Put(ctx, tx, "sources", src.ID, src); e != nil {
				return e
			}
		}
		catalogs, e := store.List[model.Catalog](ctx, tx, "catalogs")
		if e != nil {
			return e
		}
		for _, c := range catalogs {
			if !c.Enabled {
				continue
			}
			due, _ := time.Parse(time.RFC3339, c.NextSync)
			if due.After(time.Now()) {
				continue
			}
			if e = enqueue(ctx, tx, model.ID("job"), "catalog.sync", c.ID); e != nil {
				return e
			}
			c.NextSync = time.Now().Add(time.Duration(c.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
			if e = store.Put(ctx, tx, "catalogs", c.ID, c); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Service) work(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, 95*time.Second)
	defer cancel()
	var id, kind, target string
	attempts := 0
	lease := model.ID("lease")
	e := s.DB.Write(ctx, func(tx *store.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE jobs SET status=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,error='worker_lease_expired',lease_token=NULL,lease_until=NULL WHERE status='running' AND lease_until<now()`); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `UPDATE jobs SET status='running',attempts=attempts+1,lease_until=now()+interval '2 minutes',lease_token=$1 WHERE id=(SELECT id FROM jobs WHERE status='queued' AND available_at<=now() ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,kind,target_id,attempts`, lease).Scan(&id, &kind, &target, &attempts)
	})
	if e != nil {
		return false
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.DB.Pool.Exec(ctx, `UPDATE jobs SET lease_until=now()+interval '2 minutes' WHERE id=$1 AND lease_token=$2 AND status='running'`, id, lease)
			}
		}
	}()
	switch kind {
	case "source.sync":
		e = s.SyncSource(ctx, target)
	case "source.probe":
		e = s.Probe(ctx, target)
	case "catalog.sync":
		e = s.SyncCatalog(ctx, target)
	case "runtime.test":
		e = s.TestRuntime(ctx, target, false)
	case "runtime.sync":
		e = s.TestRuntime(ctx, target, true)
	default:
		e = errors.New("unsupported job")
	}
	close(done)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer finishCancel()
	status, code := "succeeded", ""
	if e != nil {
		status = "queued"
		code = "upstream_or_validation_failed"
		if errors.Is(e, store.ErrNotFound) || errors.Is(e, pgx.ErrNoRows) {
			code = "target_not_found"
			attempts = 3
		}
		if errors.Is(e, ErrConflict) {
			code = "configuration_changed"
		}
		if attempts >= 3 {
			status = "failed"
		}
	}
	_, _ = s.DB.Pool.Exec(finishCtx, `UPDATE jobs SET status=$3,error=$4,available_at=now()+make_interval(secs => $5),lease_until=NULL,lease_token=NULL,finished_at=CASE WHEN $3 IN ('succeeded','failed') THEN now() ELSE NULL END WHERE id=$1 AND lease_token=$2`, id, lease, status, code, attempts*attempts*15)
	return true
}
