package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Cylunex/shadow-relay/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var tables = map[string]bool{"feedback": true, "catalogs": true, "runtimes": true, "sources": true, "endpoints": true, "secrets": true, "revisions": true, "candidates": true, "probes": true, "source_sets": true, "publications": true, "bindings": true, "audits": true}

type DB struct{ Pool *pgxpool.Pool }

// Reader is shared by the pool and a transaction; identifiers are never supplied by callers outside this package's allowlist.
type Reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Tx struct{ pgx.Tx }

func Open(ctx context.Context, dsn string) (*DB, error) {
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	d := &DB{p}
	return d, nil
}
func (d *DB) Migrate(ctx context.Context) error {
	return d.Write(ctx, func(tx *Tx) error {
		if _, e := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
			return e
		}
		files, e := migrations.Files.ReadDir(".")
		if e != nil {
			return e
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			var exists bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)", f.Name()).Scan(&exists); e != nil {
				return e
			}
			if exists {
				continue
			}
			b, e := migrations.Files.ReadFile(f.Name())
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, string(b)); e != nil {
				return fmt.Errorf("migration %s: %w", f.Name(), e)
			}
			if _, e = tx.Exec(ctx, "INSERT INTO schema_migrations(name) VALUES($1)", f.Name()); e != nil {
				return e
			}
		}
		return nil
	})
}
func (d *DB) Write(ctx context.Context, fn func(*Tx) error) error {
	t, e := d.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer t.Rollback(ctx)
	// Short control-plane transactions are serialized across API and worker processes.
	if _, e = t.Exec(ctx, "SELECT pg_advisory_xact_lock(73194021)"); e != nil {
		return e
	}
	if e = fn(&Tx{t}); e != nil {
		return e
	}
	return t.Commit(ctx)
}
func table(t string) string {
	if !tables[t] {
		panic("invalid table")
	}
	return t
}
func Get[T any](ctx context.Context, q Reader, t, id string) (T, error) {
	var v T
	var b []byte
	e := q.QueryRow(ctx, "SELECT data FROM "+table(t)+" WHERE id=$1", id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v)
	return v, e
}
func List[T any](ctx context.Context, q Reader, t string) ([]T, error) {
	rows, e := q.Query(ctx, "SELECT data FROM "+table(t)+" ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b []byte
		var v T
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func Put(ctx context.Context, tx *Tx, t, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+table(t)+"(id,data) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET data=excluded.data", id, b)
	return e
}
func Insert(ctx context.Context, tx *Tx, t, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+table(t)+"(id,data) VALUES($1,$2)", id, b)
	return e
}
func Delete(ctx context.Context, tx *Tx, t, id string) error {
	tag, e := tx.Exec(ctx, "DELETE FROM "+table(t)+" WHERE id=$1", id)
	if e == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return e
}
