// Package testutil provides isolated PostgreSQL schemas for integration tests.
package testutil

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/jackc/pgx/v5"
)

func Database(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is unset; PostgreSQL integration test skipped")
	}
	ctx := context.Background()
	admin, e := store.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := model.ID("test")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Pool.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Pool.Close()
		_, _ = admin.Pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Pool.Close()
	})
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatalf("migration is not idempotent: %v", e)
	}
	return db
}
