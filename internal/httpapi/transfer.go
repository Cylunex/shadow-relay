package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Cylunex/shadow-relay/internal/transfer"
)

func (s *Server) transferRoutes(mux *http.ServeMux) {
	manager := &transfer.Manager{DB: s.Service.DB, Vault: s.Service.Vault}
	mux.HandleFunc("GET /api/v1/data/export", handle(func(w http.ResponseWriter, r *http.Request) error {
		if !s.transferMu.TryLock() {
			reply(w, 409, map[string]string{"error": "已有导入或导出正在执行"})
			return nil
		}
		defer s.transferMu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		file, err := manager.Export(ctx)
		if err != nil {
			return err
		}
		defer os.Remove(file)
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="shadow-relay-data-`+time.Now().UTC().Format("20060102T150405Z")+`.tar.gz"`)
		http.ServeFile(w, r, file)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/data/import", handle(func(w http.ResponseWriter, r *http.Request) error {
		if !s.transferMu.TryLock() {
			reply(w, 409, map[string]string{"error": "已有导入或导出正在执行"})
			return nil
		}
		defer s.transferMu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, transfer.MaxUpload+(1<<20))
		multipart, err := r.MultipartReader()
		if err != nil {
			return errors.New("请使用 multipart/form-data 上传备份文件")
		}
		upload, err := os.CreateTemp(s.Service.Vault.Dir, ".upload-*.tar.gz")
		if err != nil {
			return err
		}
		defer os.Remove(upload.Name())
		defer upload.Close()
		fields := map[string]bool{}
		mode, key := "preview", ""
		for {
			part, err := multipart.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				reply(w, 413, map[string]string{"error": "上传无效或超过 64 MiB"})
				return nil
			}
			name := part.FormName()
			if fields[name] {
				return errors.New("不允许重复的上传字段")
			}
			fields[name] = true
			if name == "file" {
				if n, err := io.Copy(upload, io.LimitReader(part, transfer.MaxUpload+1)); err != nil || n > transfer.MaxUpload {
					reply(w, 413, map[string]string{"error": "上传无效或超过 64 MiB"})
					return nil
				}
			} else {
				b, err := io.ReadAll(io.LimitReader(part, 1025))
				if err != nil || len(b) > 1024 {
					return errors.New("上传字段过长")
				}
				switch name {
				case "mode":
					mode = string(b)
				case "sourceMasterKey":
					key = strings.TrimSpace(string(b))
				default:
					return errors.New("未知的上传字段")
				}
			}
			part.Close()
		}
		if !fields["file"] || (mode != "preview" && mode != "apply") {
			return errors.New("需要 file 文件和 preview/apply 模式")
		}
		if _, err := upload.Seek(0, io.SeekStart); err != nil {
			return err
		}
		p, err := manager.Prepare(ctx, upload, key)
		if err != nil {
			return err
		}
		defer p.Close()
		summary, err := manager.Import(ctx, p, mode == "preview")
		if errors.Is(err, transfer.ErrBusy) || errors.Is(err, transfer.ErrConflict) {
			reply(w, 409, map[string]string{"error": err.Error()})
			return nil
		}
		if err != nil {
			return err
		}
		reply(w, 200, summary)
		return nil
	}))
}
