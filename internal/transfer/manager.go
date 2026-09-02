package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/Cylunex/shadow-relay/migrations"
	"github.com/jackc/pgx/v5"
)

var ErrConflict = errors.New("已有同 ID 数据或种子文件与备份不同；未导入任何记录")
var ErrBusy = errors.New("存在待执行或正在执行的任务，请待任务结束后导入")
var dryRun = errors.New("preview transaction rollback")

type Manager struct {
	DB    *store.DB
	Vault *security.Vault
}
type Summary struct {
	Format          string         `json:"format"`
	Counts          map[string]int `json:"counts"`
	Added           map[string]int `json:"added"`
	Reused          map[string]int `json:"reused"`
	Snapshots       int            `json:"snapshots"`
	SeedFiles       int            `json:"seedFiles"`
	InterruptedJobs int            `json:"interruptedJobs"`
	KeyRequired     bool           `json:"keyRequired"`
	Applied         bool           `json:"applied"`
}
type Prepared struct {
	Dir     string
	Doc     *Document
	Files   map[string]string
	Summary Summary
}

func (p *Prepared) Close() { _ = os.RemoveAll(p.Dir) }

func (m *Manager) Prepare(ctx context.Context, reader io.Reader, sourceKey string) (_ *Prepared, err error) {
	dir, err := os.MkdirTemp(m.Vault.Dir, ".transfer-")
	if err != nil {
		return nil, err
	}
	p := &Prepared{Dir: dir}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	p.Doc, p.Files, err = unpack(ctx, reader, dir)
	if err != nil {
		return nil, err
	}
	p.Summary = Summary{Format: p.Doc.Format, Counts: map[string]int{}, Added: map[string]int{}, Reused: map[string]int{}}
	known, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, migration := range p.Doc.Migrations {
		if !slices.ContainsFunc(known, func(f fs.DirEntry) bool { return f.Name() == migration }) {
			return nil, errors.New("备份来自不兼容的数据库版本")
		}
	}
	if len(p.Doc.Tables) != len(tables) || len(p.Doc.Migrations) == 0 {
		return nil, errors.New("备份缺少必需的数据表或迁移记录")
	}
	total := len(p.Doc.Jobs)
	for table, records := range p.Doc.Tables {
		if !slices.Contains(tables, table) {
			return nil, errors.New("备份包含未知数据库表")
		}
		ids := map[string]bool{}
		for _, r := range records {
			var identity struct {
				ID string `json:"id"`
			}
			if !recordID.MatchString(r.ID) || ids[r.ID] || json.Unmarshal(r.Data, &identity) != nil || identity.ID != r.ID {
				return nil, errors.New("备份记录 ID 无效或重复")
			}
			ids[r.ID] = true
		}
		total += len(records)
		p.Summary.Counts[table] = len(records)
	}
	if total > 100000 {
		return nil, errors.New("备份记录数量超过上限")
	}
	source := m.Vault
	if sourceKey != "" {
		source, err = security.NewVault(sourceKey, filepath.Join(dir, "source-key"))
		if err != nil {
			return nil, errors.New("原实例主密钥格式无效")
		}
	}
	for name, file := range p.Files {
		if seedName(name) {
			p.Summary.SeedFiles++
			continue
		}
		if !strings.HasPrefix(name, "data/snapshots/") {
			continue
		}
		p.Summary.Snapshots++
		cipher, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		hash := filepath.Base(file)
		plain, err := source.Open(string(cipher), hash)
		if err != nil {
			p.Summary.KeyRequired = true
			continue
		}
		if security.Hash(plain) != hash {
			return nil, errors.New("备份快照校验和不匹配")
		}
		plainDir := filepath.Join(dir, "plain")
		if err := os.MkdirAll(plainDir, 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(plainDir, hash), plain, 0600); err != nil {
			return nil, err
		}
	}
	for _, r := range p.Doc.Tables["revisions"] {
		var revision model.Revision
		if json.Unmarshal(r.Data, &revision) != nil || !hashName.MatchString(revision.Hash) || p.Files["data/snapshots/"+revision.Hash] == "" {
			return nil, errors.New("备份缺少版本引用的快照")
		}
	}
	for i, r := range p.Doc.Tables["secrets"] {
		var secret model.Secret
		if json.Unmarshal(r.Data, &secret) != nil || secret.OwnerID == "" {
			return nil, errors.New("备份凭据记录无效")
		}
		plain, err := source.Open(secret.Ciphertext, secret.OwnerID)
		if err != nil {
			p.Summary.KeyRequired = true
			continue
		}
		var headers map[string]string
		if json.Unmarshal(plain, &headers) != nil || security.ValidateHeaders(headers) != nil {
			return nil, errors.New("备份中的凭据内容无效")
		}
		secret.Ciphertext, err = m.Vault.Seal(plain, secret.OwnerID)
		if err != nil {
			return nil, err
		}
		p.Doc.Tables["secrets"][i].Data, _ = json.Marshal(secret)
	}
	jobIDs := map[string]bool{}
	for i, b := range p.Doc.Jobs {
		var job map[string]any
		if json.Unmarshal(b, &job) != nil {
			return nil, errors.New("任务记录无效")
		}
		id, _ := job["id"].(string)
		status, _ := job["status"].(string)
		if !recordID.MatchString(id) || jobIDs[id] || !slices.Contains([]string{"queued", "running", "succeeded", "failed"}, status) {
			return nil, errors.New("任务状态或 ID 无效")
		}
		jobIDs[id] = true
		if status == "running" || status == "queued" {
			job["status"] = "failed"
			job["error"] = "interrupted by data migration; retry explicitly"
			job["lease_until"] = nil
			job["lease_token"] = nil
			job["finished_at"] = job["created_at"]
			p.Summary.InterruptedJobs++
		}
		p.Doc.Jobs[i], _ = json.Marshal(job)
	}
	p.Summary.Counts["jobs"] = len(p.Doc.Jobs)
	return p, nil
}

// Live seed files are mutable data, so they follow the vault across releases.
func (m *Manager) SeedDir() string {
	if dir := os.Getenv("RELAY_SEEDS_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(m.Vault.Dir, "seeds")
}

func (m *Manager) Import(ctx context.Context, p *Prepared, preview bool) (Summary, error) {
	p.Summary.Added = map[string]int{}
	p.Summary.Reused = map[string]int{}
	p.Summary.Applied = false
	if p.Summary.KeyRequired {
		if preview {
			return p.Summary, nil
		}
		return p.Summary, errors.New("无法解密备份；请提供正确的原实例主密钥，当前数据未修改")
	}
	seedTarget := m.SeedDir()
	seedStage := filepath.Join(p.Dir, "merged-seeds")
	seedBackup := filepath.Join(p.Dir, "previous-seeds")
	seedSwitched, hadSeeds := false, false
	defer func() {
		if seedSwitched && !p.Summary.Applied {
			_ = os.RemoveAll(seedTarget)
			if hadSeeds {
				_ = os.Rename(seedBackup, seedTarget)
			}
		}
	}()
	err := m.DB.Write(ctx, func(tx *store.Tx) error {
		var active int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE status IN ('queued','running')").Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return ErrBusy
		}
		for _, table := range tables {
			for _, r := range p.Doc.Tables[table] {
				var identical bool
				err := tx.QueryRow(ctx, "SELECT data=$2::jsonb FROM "+table+" WHERE id=$1", r.ID, r.Data).Scan(&identical)
				if err == nil && !identical && table == "secrets" {
					existing, e := store.Get[model.Secret](ctx, tx, table, r.ID)
					if e != nil {
						return e
					}
					var incoming model.Secret
					_ = json.Unmarshal(r.Data, &incoming)
					a, ea := m.Vault.Open(existing.Ciphertext, existing.OwnerID)
					b, eb := m.Vault.Open(incoming.Ciphertext, incoming.OwnerID)
					identical = ea == nil && eb == nil && bytes.Equal(a, b) && existing.OwnerID == incoming.OwnerID && existing.UpdatedAt == incoming.UpdatedAt
				}
				if err == nil {
					if !identical {
						return ErrConflict
					}
					p.Summary.Reused[table]++
					continue
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if err := store.Insert(ctx, tx, table, r.ID, r.Data); err != nil {
					return err
				}
				p.Summary.Added[table]++
			}
		}
		for _, job := range p.Doc.Jobs {
			var record struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(job, &record)
			var identical bool
			err := tx.QueryRow(ctx, "SELECT to_jsonb(j)=to_jsonb(json_populate_record(NULL::jobs,$2::json)) FROM jobs j WHERE id=$1", record.ID, job).Scan(&identical)
			if err == nil {
				if !identical {
					return ErrConflict
				}
				p.Summary.Reused["jobs"]++
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO jobs SELECT * FROM json_populate_record(NULL::jobs,$1::json)", job); err != nil {
				return err
			}
			p.Summary.Added["jobs"]++
		}
		if p.Summary.SeedFiles > 0 {
			if err := os.MkdirAll(seedStage, 0700); err != nil {
				return err
			}
			if info, err := os.Lstat(seedTarget); err == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("种子目录不能是符号链接")
				}
				hadSeeds = true
				if err := copySeeds(seedTarget, seedStage); err != nil {
					return err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			for name, source := range p.Files {
				if !seedName(name) {
					continue
				}
				b, err := os.ReadFile(source)
				if err != nil {
					return err
				}
				target := filepath.Join(seedStage, filepath.FromSlash(strings.TrimPrefix(name, "seeds/")))
				if existing, err := os.ReadFile(target); err == nil {
					if !bytes.Equal(existing, b) {
						return ErrConflict
					}
					continue
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					return err
				}
				if err := os.WriteFile(target, b, 0600); err != nil {
					return err
				}
			}
		}
		if preview {
			return dryRun
		}
		for name := range p.Files {
			if !strings.HasPrefix(name, "data/snapshots/") {
				continue
			}
			hash := filepath.Base(name)
			if _, err := os.Lstat(filepath.Join(m.Vault.Dir, "snapshots", hash)); err == nil {
				if _, err := m.Vault.ReadSnapshot(hash); err != nil {
					return errors.New("现有快照校验失败，导入已取消")
				}
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			plain, err := os.ReadFile(filepath.Join(p.Dir, "plain", hash))
			if err != nil {
				return err
			}
			if _, err := m.Vault.Snapshot(plain); err != nil {
				return err
			}
		}
		if p.Summary.SeedFiles > 0 {
			if hadSeeds {
				if err := os.Rename(seedTarget, seedBackup); err != nil {
					return err
				}
			}
			if err := os.Rename(seedStage, seedTarget); err != nil {
				if hadSeeds {
					_ = os.Rename(seedBackup, seedTarget)
				}
				return err
			}
			seedSwitched = true
		}
		a := model.Audit{ID: model.ID("audit"), Action: "data.import", TargetID: "workspace", CreatedAt: model.Now()}
		return store.Insert(ctx, tx, "audits", a.ID, a)
	})
	if errors.Is(err, dryRun) {
		return p.Summary, nil
	}
	if err == nil {
		p.Summary.Applied = true
	}
	return p.Summary, err
}

func copySeeds(source, destination string) error {
	return filepath.WalkDir(source, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("种子目录含符号链接")
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(destination, rel), 0700)
		}
		if !d.Type().IsRegular() || !seedName("seeds/"+filepath.ToSlash(rel)) {
			return errors.New("种子目录含不支持的文件")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFile {
			return errors.New("种子文件过大")
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, rel), b, 0600)
	})
}

func (m *Manager) Export(ctx context.Context) (file string, err error) {
	out, err := os.CreateTemp(m.Vault.Dir, ".export-*.tar.gz")
	if err != nil {
		return "", err
	}
	file = out.Name()
	defer func() {
		_ = out.Close()
		if err != nil {
			_ = os.Remove(file)
		}
	}()
	err = m.DB.Write(ctx, func(tx *store.Tx) error {
		doc := Document{Format: Format, CreatedAt: model.Now(), Tables: map[string][]Record{}}
		for _, table := range tables {
			doc.Tables[table] = []Record{}
			rows, err := tx.Query(ctx, "SELECT id,data FROM "+table+" ORDER BY id")
			if err != nil {
				return err
			}
			for rows.Next() {
				var r Record
				if err := rows.Scan(&r.ID, &r.Data); err != nil {
					rows.Close()
					return err
				}
				doc.Tables[table] = append(doc.Tables[table], r)
			}
			rows.Close()
			if rows.Err() != nil {
				return rows.Err()
			}
		}
		rows, err := tx.Query(ctx, "SELECT name FROM schema_migrations ORDER BY name")
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			doc.Migrations = append(doc.Migrations, name)
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		rows, err = tx.Query(ctx, "SELECT to_jsonb(j) FROM jobs j ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var b json.RawMessage
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				return err
			}
			doc.Jobs = append(doc.Jobs, b)
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		gz := gzip.NewWriter(&cappedWriter{w: out, remaining: MaxUpload})
		tw := tar.NewWriter(gz)
		expanded := int64(0)
		count := 0
		add := func(name string, b []byte) error {
			expanded += int64(len(b)) + 1024
			count++
			if len(b) > maxFile || expanded > maxExpanded-4096 || count > 9999 {
				return errors.New("数据量超过单次导出限制")
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(b)), ModTime: time.Now()}); err != nil {
				return err
			}
			_, err := tw.Write(b)
			return err
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err := add("manifest.json", b); err != nil {
			return err
		}
		hashes := map[string]bool{}
		for _, r := range doc.Tables["revisions"] {
			var revision model.Revision
			if json.Unmarshal(r.Data, &revision) != nil {
				return errors.New("版本记录无效")
			}
			hashes[revision.Hash] = true
		}
		for hash := range hashes {
			if _, err := m.Vault.ReadSnapshot(hash); err != nil {
				return errors.New("快照不完整，无法生成可恢复的备份")
			}
			b, err := os.ReadFile(filepath.Join(m.Vault.Dir, "snapshots", hash))
			if err != nil {
				return err
			}
			if err := add("data/snapshots/"+hash, b); err != nil {
				return err
			}
		}
		seedDir := m.SeedDir()
		if _, err := os.Stat(seedDir); errors.Is(err, os.ErrNotExist) && os.Getenv("RELAY_SEEDS_DIR") == "" {
			seedDir = "seeds"
		}
		if _, err := os.Stat(seedDir); err == nil {
			if err := filepath.WalkDir(seedDir, func(file string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(seedDir, file)
				if err != nil {
					return err
				}
				name := "seeds/" + filepath.ToSlash(rel)
				if !d.Type().IsRegular() || !seedName(name) {
					return errors.New("种子目录含不支持的文件")
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				if info.Size() > maxFile {
					return errors.New("种子文件过大")
				}
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				return add(name, b)
			}); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := tw.Close(); err != nil {
			return err
		}
		if err := gz.Close(); err != nil {
			return err
		}
		if err := out.Sync(); err != nil {
			return err
		}
		a := model.Audit{ID: model.ID("audit"), Action: "data.export", TargetID: "workspace", CreatedAt: model.Now()}
		return store.Insert(ctx, tx, "audits", a.ID, a)
	})
	return file, err
}
