package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/Cylunex/shadow-relay/internal/testutil"
)

func vault(t *testing.T) (*security.Vault, string) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	encoded := base64.StdEncoding.EncodeToString(key)
	v, err := security.NewVault(encoded, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v, encoded
}
func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	v, key := vault(t)
	return &Manager{DB: testutil.Database(t), Vault: v}, key
}
func doc() Document {
	d := Document{Format: Format, Migrations: []string{"001_initial.sql", "002_feedback.sql"}, Tables: map[string][]Record{}}
	for _, table := range tables {
		d.Tables[table] = []Record{}
	}
	return d
}
func bundle(t *testing.T, d Document, extra map[string][]byte) []byte {
	t.Helper()
	b, _ := json.Marshal(d)
	entries := map[string][]byte{"manifest.json": b}
	for k, v := range extra {
		entries[k] = v
	}
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, data := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0600}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func record(id string, value any) Record { b, _ := json.Marshal(value); return Record{ID: id, Data: b} }

func TestArchiveRejectsUnsafePathsLinksDuplicatesAndCorruption(t *testing.T) {
	for _, name := range []string{"../escape", "/tmp/escape", "data/snapshots/../../escape", `seeds\escape.txt`, "seeds/.env"} {
		if _, _, err := unpack(context.Background(), bytes.NewReader(bundle(t, doc(), map[string][]byte{name: []byte("x")})), t.TempDir()); err == nil {
			t.Fatalf("accepted unsafe path %q", name)
		}
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "seeds/file.txt", Typeflag: kind, Linkname: "/etc/passwd"})
		_ = tw.Close()
		_ = gz.Close()
		if _, _, err := unpack(context.Background(), &b, t.TempDir()); err == nil {
			t.Fatal("accepted link")
		}
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for range 2 {
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: 2})
		_, _ = tw.Write([]byte("{}"))
	}
	_ = tw.Close()
	_ = gz.Close()
	if _, _, err := unpack(context.Background(), &b, t.TempDir()); err == nil {
		t.Fatal("accepted duplicate")
	}
	corrupt := bundle(t, doc(), nil)
	corrupt[len(corrupt)-5] ^= 255
	if _, _, err := unpack(context.Background(), bytes.NewReader(corrupt), t.TempDir()); err == nil {
		t.Fatal("accepted corrupt gzip")
	}
}

func TestCOPYReaderDecodesDataWithoutExecutingSQL(t *testing.T) {
	input := "DROP DATABASE postgres;\nCOPY public.sources (id, data) FROM stdin;\nsource_one\t{\"id\":\"source_one\",\"name\":\"A\\\\nB\"}\n\\.\n"
	d, err := parseCOPY(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := json.Unmarshal(d.Tables["sources"][0].Data, &value); err != nil {
		t.Fatal(err)
	}
	if value["name"] != "A\nB" {
		t.Fatal("COPY escaping changed data")
	}
	for _, s := range []string{"COPY public.users (id, data) FROM stdin;\n\\.\n", "COPY public.sources (data, id) FROM stdin;\n\\.\n", "COPY public.sources (id, data) FROM stdin;\n"} {
		if _, err := parseCOPY(strings.NewReader(s)); err == nil {
			t.Fatal("accepted incompatible COPY")
		}
	}
}

func TestWrongKeyAndMissingSnapshotDoNotModifyDestination(t *testing.T) {
	source, _ := vault(t)
	target, _ := vault(t)
	m := &Manager{Vault: target}
	hash, _ := source.Snapshot([]byte("private source"))
	cipher, _ := os.ReadFile(filepath.Join(source.Dir, "snapshots", hash))
	d := doc()
	d.Tables["revisions"] = []Record{record("revision_one", model.Revision{ID: "revision_one", Hash: hash})}
	p, err := m.Prepare(context.Background(), bytes.NewReader(bundle(t, d, map[string][]byte{"data/snapshots/" + hash: cipher})), "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if !p.Summary.KeyRequired {
		t.Fatal("wrong key was accepted")
	}
	if _, err := m.Import(context.Background(), p, false); err == nil {
		t.Fatal("wrong key imported")
	}
	entries, _ := os.ReadDir(filepath.Join(target.Dir, "snapshots"))
	if len(entries) != 0 {
		t.Fatal("destination changed")
	}
	if _, err := m.Prepare(context.Background(), bytes.NewReader(bundle(t, d, nil)), ""); err == nil {
		t.Fatal("missing snapshot accepted")
	}
}

func TestRoundTripResealsSecretsAndSnapshotsWithPreviewAndIdempotency(t *testing.T) {
	ctx := context.Background()
	source, sourceKey := fixture(t)
	target, _ := fixture(t)
	raw := []byte("private fixture body")
	hash, _ := source.Vault.Snapshot(raw)
	cipher, _ := source.Vault.Seal([]byte(`{"Authorization":"fixture-private-header"}`), "source_one")
	err := source.DB.Write(ctx, func(tx *store.Tx) error {
		for _, v := range []struct {
			table, id string
			data      any
		}{
			{"sources", "source_one", model.Source{ID: "source_one", Name: "Fixture"}},
			{"revisions", "revision_one", model.Revision{ID: "revision_one", SourceID: "source_one", Hash: hash}},
			{"secrets", "source_one", model.Secret{ID: "source_one", OwnerID: "source_one", Ciphertext: cipher}},
		} {
			if err := store.Insert(ctx, tx, v.table, v.id, v.data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source.SeedDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.SeedDir(), "recipes.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := source.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)
	f, _ := os.Open(file)
	p, err := target.Prepare(ctx, f, sourceKey)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Summary.KeyRequired {
		t.Fatal("valid key refused")
	}
	summary, err := target.Import(ctx, p, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Applied || summary.Added["sources"] != 1 {
		t.Fatal(summary)
	}
	var count int
	_ = target.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM sources").Scan(&count)
	if count != 0 {
		t.Fatal("preview committed")
	}
	if _, err := os.Stat(target.SeedDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview changed seeds")
	}
	summary, err = target.Import(ctx, p, false)
	if err != nil || !summary.Applied || summary.Added["sources"] != 1 {
		t.Fatal(summary, err)
	}
	got, err := target.Vault.ReadSnapshot(hash)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("snapshot did not migrate", err)
	}
	secret, err := store.Get[model.Secret](ctx, target.DB.Pool, "secrets", "source_one")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Ciphertext == cipher {
		t.Fatal("credential was not resealed")
	}
	if _, err := target.Vault.Open(secret.Ciphertext, "source_one"); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Open(file)
	again, err := target.Prepare(ctx, f, sourceKey)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	summary, err = target.Import(ctx, again, false)
	if err != nil || summary.Reused["sources"] != 1 || summary.Reused["secrets"] != 1 {
		t.Fatal(summary, err)
	}
	exported, err := target.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(exported)
	f, _ = os.Open(exported)
	sameKey, err := target.Prepare(ctx, f, "")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer sameKey.Close()
	if sameKey.Summary.KeyRequired || sameKey.Summary.Snapshots != 1 || sameKey.Summary.SeedFiles != 1 {
		t.Fatal("export is not restorable")
	}
}

func TestConflictRollsBackAllRowsAndSeeds(t *testing.T) {
	ctx := context.Background()
	m, _ := fixture(t)
	if err := m.DB.Write(ctx, func(tx *store.Tx) error {
		return store.Insert(ctx, tx, "sources", "source_existing", model.Source{ID: "source_existing", Name: "Keep"})
	}); err != nil {
		t.Fatal(err)
	}
	d := doc()
	d.Tables["sources"] = []Record{record("source_new", model.Source{ID: "source_new", Name: "New"}), record("source_existing", model.Source{ID: "source_existing", Name: "Replace"})}
	p, err := m.Prepare(ctx, bytes.NewReader(bundle(t, d, map[string][]byte{"seeds/recipes.json": []byte(`[]`)})), "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := m.Import(ctx, p, false); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := store.Get[model.Source](ctx, m.DB.Pool, "sources", "source_new"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial import")
	}
	if _, err := os.Stat(m.SeedDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("seeds changed on conflict")
	}
}

func TestProvidedArchive(t *testing.T) {
	file := os.Getenv("RELAY_TEST_ARCHIVE")
	keyFile := os.Getenv("RELAY_TEST_SOURCE_KEY_FILE")
	if file == "" || keyFile == "" {
		t.Skip("operator archive not configured")
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal("source key unavailable")
	}
	m, _ := fixture(t)
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Prepare(context.Background(), f, strings.TrimSpace(string(key)))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Summary.KeyRequired {
		t.Fatal("source key does not decrypt the archive")
	}
	summary, err := m.Import(context.Background(), p, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Validated archive: %d sources, %d revisions, %d snapshots, %d seed files", summary.Counts["sources"], summary.Counts["revisions"], summary.Snapshots, summary.SeedFiles)
}
