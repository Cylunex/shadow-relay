// Package transfer implements portable workspace archives and a data-only legacy reader.
package transfer

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const MaxUpload = 64 << 20
const maxExpanded = 128 << 20
const maxFile = 32 << 20
const Format = "shadow-relay-data/v1"

var tables = []string{"runtimes", "catalogs", "sources", "endpoints", "secrets", "revisions", "candidates", "probes", "source_sets", "publications", "bindings", "feedback", "audits"}
var hashName = regexp.MustCompile(`^[a-f0-9]{64}$`)
var recordID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,200}$`)

type Record struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}
type Document struct {
	Format     string              `json:"format"`
	CreatedAt  string              `json:"createdAt"`
	Migrations []string            `json:"migrations"`
	Tables     map[string][]Record `json:"tables"`
	Jobs       []json.RawMessage   `json:"jobs"`
}

func safeName(name string) (string, bool) {
	name = strings.TrimSuffix(name, "/")
	if name == "" || len(name) > 240 || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return name, true
}
func seedName(name string) bool {
	_, ok := safeName(name)
	return ok && strings.HasPrefix(name, "seeds/") && slices.Contains([]string{".json", ".txt", ".md"}, strings.ToLower(path.Ext(name)))
}

func unpack(ctx context.Context, input io.Reader, dir string) (*Document, map[string]string, error) {
	compressed := &io.LimitedReader{R: input, N: MaxUpload + 1}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, nil, errors.New("需要有效的 .tar.gz 数据包")
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	reader := tar.NewReader(expanded)
	files := map[string]string{}
	seen := map[string]bool{}
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || count >= 10000 {
			return nil, nil, errors.New("压缩包无效或文件数量超限")
		}
		name, valid := safeName(h.Name)
		if !valid || seen[name] {
			return nil, nil, errors.New("压缩包包含不安全路径或重复文件")
		}
		seen[name] = true
		if h.Typeflag == tar.TypeDir {
			if name != "data" && name != "data/snapshots" && name != "seeds" && !strings.HasPrefix(name, "seeds/") {
				return nil, nil, errors.New("压缩包包含未知目录")
			}
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > maxFile {
			return nil, nil, errors.New("仅支持普通文件，单文件上限 32 MiB")
		}
		allowed := name == "manifest.json" || name == "relay.pgdump" || seedName(name) || (strings.HasPrefix(name, "data/snapshots/") && hashName.MatchString(strings.TrimPrefix(name, "data/snapshots/")))
		if !allowed {
			return nil, nil, errors.New("压缩包包含未知文件")
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, nil, err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, nil, err
		}
		_, copyErr := io.Copy(f, reader)
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return nil, nil, errors.New("无法读取备份文件")
		}
		files[name] = target
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil || expanded.N <= 0 || compressed.N <= 0 {
		return nil, nil, errors.New("压缩包校验失败或解压后超过 128 MiB")
	}
	if files["manifest.json"] != "" && files["relay.pgdump"] != "" {
		return nil, nil, errors.New("压缩包包含冲突的数据格式")
	}
	var doc *Document
	if file := files["manifest.json"]; file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, err
		}
		doc = &Document{}
		if json.Unmarshal(b, doc) != nil || doc.Format != Format {
			return nil, nil, errors.New("不支持的数据包版本")
		}
	} else if file := files["relay.pgdump"]; file != "" {
		var err error
		doc, err = legacy(ctx, file, dir)
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, errors.New("数据包缺少 manifest.json 或 relay.pgdump")
	}
	return doc, files, nil
}

type cappedWriter struct {
	w         io.Writer
	remaining int64
}

func (w *cappedWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, errors.New("备份数据超过大小限制")
	}
	n, err := w.w.Write(b)
	w.remaining -= int64(n)
	return n, err
}

// pg_restore is only a format decoder: no connection, SQL execution, or shell.
func legacy(ctx context.Context, file, dir string) (*Document, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 5)
	_, err = io.ReadFull(f, header)
	f.Close()
	if err != nil {
		return nil, errors.New("数据库备份无效")
	}
	if string(header) == "PGDMP" {
		binary := os.Getenv("RELAY_PG_RESTORE")
		if binary == "" {
			binary = "pg_restore"
		}
		decoded := filepath.Join(dir, "decoded.sql")
		out, err := os.OpenFile(decoded, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, binary, "--data-only", "--no-owner", "--no-privileges", "--file=-", file)
		cmd.Stdout = &cappedWriter{w: out, remaining: maxExpanded}
		cmd.Stderr = io.Discard
		err = cmd.Run()
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			return nil, errors.New("无法解码旧备份；请配置兼容版本的 RELAY_PG_RESTORE（此备份需要 PostgreSQL 17+）")
		}
		file = decoded
	} else if !strings.HasPrefix(string(header), "--") {
		return nil, errors.New("不支持的数据库备份格式")
	}
	f, err = os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseCOPY(f)
}

var copyHeader = regexp.MustCompile(`^COPY public\.([a-z_]+) \(([^)]+)\) FROM stdin;$`)
var jobColumns = []string{"id", "kind", "target_id", "status", "attempts", "available_at", "lease_until", "lease_token", "error", "created_at", "finished_at"}

func parseCOPY(input io.Reader) (*Document, error) {
	doc := &Document{Format: Format, Tables: map[string][]Record{}}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 65536), maxFile)
	table := ""
	columns := []string{}
	seen := map[string]bool{}
	for scan.Scan() {
		line := scan.Text()
		if table == "" {
			if !strings.HasPrefix(line, "COPY ") {
				continue
			}
			match := copyHeader.FindStringSubmatch(line)
			if match == nil || seen[match[1]] {
				return nil, errors.New("备份 COPY 结构无效")
			}
			table = match[1]
			columns = strings.Split(match[2], ", ")
			seen[table] = true
			expected := []string{"id", "data"}
			if table == "jobs" {
				expected = jobColumns
			} else if table == "schema_migrations" {
				expected = []string{"name", "applied_at"}
			} else if !slices.Contains(tables, table) {
				return nil, errors.New("备份包含未知数据库表")
			}
			if !slices.Equal(columns, expected) {
				return nil, errors.New("备份数据库列与当前版本不兼容")
			}
			if slices.Contains(tables, table) {
				doc.Tables[table] = []Record{}
			}
			continue
		}
		if line == `\.` {
			table = ""
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != len(columns) {
			return nil, errors.New("备份 COPY 数据无效")
		}
		values := make([]*string, len(fields))
		for i, field := range fields {
			if field == `\N` {
				continue
			}
			value, err := copyValue(field)
			if err != nil {
				return nil, err
			}
			values[i] = &value
		}
		if values[0] == nil {
			return nil, errors.New("备份记录缺少 ID")
		}
		switch table {
		case "schema_migrations":
			doc.Migrations = append(doc.Migrations, *values[0])
		case "jobs":
			job := map[string]any{}
			for i, value := range values {
				if value == nil {
					job[columns[i]] = nil
				} else {
					job[columns[i]] = *value
				}
			}
			if values[4] == nil {
				return nil, errors.New("任务记录无效")
			}
			n, err := strconv.Atoi(*values[4])
			if err != nil {
				return nil, errors.New("任务次数无效")
			}
			job["attempts"] = n
			b, _ := json.Marshal(job)
			doc.Jobs = append(doc.Jobs, b)
		default:
			if values[1] == nil || !json.Valid([]byte(*values[1])) {
				return nil, errors.New("备份记录 JSON 无效")
			}
			doc.Tables[table] = append(doc.Tables[table], Record{ID: *values[0], Data: json.RawMessage(*values[1])})
		}
	}
	if scan.Err() != nil || table != "" || len(seen) == 0 {
		return nil, errors.New("数据库备份被截断或不是支持的 COPY 格式")
	}
	return doc, nil
}

func copyValue(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i == len(s) {
			return "", errors.New("COPY 转义无效")
		}
		switch s[i] {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		default:
			if s[i] >= '0' && s[i] <= '7' || s[i] == 'x' {
				base, start, end := 8, i, i
				if s[i] == 'x' {
					base = 16
					start++
					end++
				}
				limit := start + 3
				if base == 16 {
					limit = start + 2
				}
				for end < len(s) && end < limit {
					if _, err := strconv.ParseUint(s[start:end+1], base, 8); err != nil {
						break
					}
					end++
				}
				if end == start {
					return "", fmt.Errorf("COPY 数值转义无效")
				}
				n, _ := strconv.ParseUint(s[start:end], base, 8)
				b.WriteByte(byte(n))
				i = end - 1
			} else {
				b.WriteByte(s[i])
			}
		}
	}
	return b.String(), nil
}
