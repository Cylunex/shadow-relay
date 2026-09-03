package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Cylunex/shadow-relay/internal/adapter"
	"github.com/Cylunex/shadow-relay/internal/model"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/service"
	"github.com/Cylunex/shadow-relay/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

type Server struct {
	Service    *service.Service
	AdminToken string
	WebDir     string
	PublicURL  string
	mu         sync.Mutex
	rates      map[string]*rate
}
type rate struct {
	Count int
	Start time.Time
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	var publicationError *service.PublicationError
	if errors.As(e, &publicationError) {
		reply(w, 400, map[string]any{"error": publicationError.Error(), "exclusions": publicationError.Exclusions, "sourceErrors": publicationError.SourceErrors})
		return
	}
	status := http.StatusBadRequest
	message := e.Error()
	if errors.Is(e, store.ErrNotFound) {
		status = 404
		message = "not found"
	}
	if errors.Is(e, service.ErrConflict) {
		status = 409
	}
	var pathErr *os.PathError
	if errors.As(e, &pathErr) {
		status = 500
		message = "storage operation failed"
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) {
		status = 409
		message = "record is referenced or conflicts with another operation"
	}
	if status == 400 && (strings.Contains(message, "connection") || strings.Contains(message, "sql") || strings.Contains(message, "snapshot") || strings.Contains(message, "encrypt")) {
		status = 500
		message = "operation failed; check service configuration"
	}
	reply(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("invalid JSON request or unknown field")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}
func handle(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if e := fn(w, r); e != nil {
			failure(w, e)
		}
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "ok", "service": "shadow-relay"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e := s.Service.DB.Pool.Ping(ctx); e != nil {
			reply(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		reply(w, 200, map[string]string{"status": "ready"})
	})
	api := http.NewServeMux()
	s.routes(api)
	mux.Handle("/api/v1/", s.auth(api))
	mux.HandleFunc("GET /p/{token}/{path...}", s.publication)
	mux.HandleFunc("POST /p/{token}/feedback", handle(func(w http.ResponseWriter, r *http.Request) error {
		if s.limited(r) {
			reply(w, 429, map[string]string{"error": "feedback rate limit exceeded"})
			return nil
		}
		var in struct {
			SourceID      string `json:"sourceId"`
			PublicationID string `json:"publicationId"`
			Code          string `json:"code"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if e := s.Service.Feedback(r.Context(), r.PathValue("token"), model.Feedback{SourceID: in.SourceID, PublicationID: in.PublicationID, Code: in.Code}); e != nil {
			return e
		}
		reply(w, 202, map[string]bool{"accepted": true})
		return nil
	}))
	mux.Handle("/", http.HandlerFunc(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		defer func() {
			if recover() != nil {
				reply(w, 500, map[string]string{"error": "internal error"})
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) limited(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rates == nil {
		s.rates = map[string]*rate{}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	v := s.rates[host]
	if v == nil || now.Sub(v.Start) > time.Minute {
		v = &rate{Start: now}
		s.rates[host] = v
	}
	v.Count++
	if len(s.rates) > 10000 {
		for k, x := range s.rates {
			if now.Sub(x.Start) > time.Minute {
				delete(s.rates, k)
			}
		}
	}
	return v.Count > 120
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		wantHash, gotHash := sha256.Sum256([]byte(s.AdminToken)), sha256.Sum256([]byte(given))
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || len(s.AdminToken) < 32 || subtle.ConstantTimeCompare(wantHash[:], gotHash[:]) != 1 {
			if s.limited(r) {
				reply(w, 429, map[string]string{"error": "too many authentication attempts"})
				return
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			reply(w, 401, map[string]string{"error": "authentication required"})
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				reply(w, 415, map[string]string{"error": "application/json required"})
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				pub, _ := url.Parse(s.PublicURL)
				if e != nil || u.Host != r.Host && (pub == nil || u.Host != pub.Host) {
					reply(w, 403, map[string]string{"error": "cross-origin mutation denied"})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
func listRoute[T any](s *Server, mux *http.ServeMux, path, table string, filter func(T) T) {
	mux.HandleFunc("GET /api/v1/"+path, handle(func(w http.ResponseWriter, r *http.Request) error {
		vs, e := store.List[T](r.Context(), s.Service.DB.Pool, table)
		if e != nil {
			return e
		}
		if filter != nil {
			for i := range vs {
				vs[i] = filter(vs[i])
			}
		}
		reply(w, 200, vs)
		return nil
	}))
}
func (s *Server) routes(mux *http.ServeMux) {
	svc := s.Service
	s.workshopRoutes(mux)
	mux.HandleFunc("GET /api/v1/proxies", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"proxyIds": svc.ProxyIDs()})
	})
	mux.HandleFunc("GET /api/v1/adapters", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"adapters": adapter.Describe(), "connectors": service.Connectors, "formats": service.Formats})
	})
	listRoute[model.Source](s, mux, "sources", "sources", nil)
	listRoute[model.Feedback](s, mux, "feedback", "feedback", nil)
	listRoute[model.Catalog](s, mux, "catalogs", "catalogs", nil)
	listRoute[model.Candidate](s, mux, "candidates", "candidates", nil)
	listRoute[model.SourceSet](s, mux, "source-sets", "source_sets", nil)
	listRoute[model.Runtime](s, mux, "runtimes", "runtimes", nil)
	listRoute[model.Binding](s, mux, "bindings", "bindings", func(b model.Binding) model.Binding { b.Hash = ""; return b })
	listRoute[model.Audit](s, mux, "audits", "audits", nil)
	mux.HandleFunc("GET /api/v1/publications", handle(func(w http.ResponseWriter, r *http.Request) error {
		ps, e := store.List[model.Publication](r.Context(), svc.DB.Pool, "publications")
		if e != nil {
			return e
		}
		for i := range ps {
			for path, a := range ps[i].Artifacts {
				a.Body = ""
				ps[i].Artifacts[path] = a
			}
		}
		reply(w, 200, ps)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/jobs", handle(func(w http.ResponseWriter, r *http.Request) error {
		jobs, e := svc.Jobs(r.Context())
		if e != nil {
			return e
		}
		reply(w, 200, jobs)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/jobs/{id}/retry", handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := svc.RetryJob(r.Context(), r.PathValue("id")); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	for _, action := range []string{"preview", "import"} {
		mux.HandleFunc("POST /api/v1/sources/"+action, handle(func(w http.ResponseWriter, r *http.Request) error {
			var in service.Input
			if e := decode(w, r, &in); e != nil {
				return e
			}
			if action == "preview" {
				n, _, e := svc.Preview(r.Context(), in)
				if e != nil {
					return e
				}
				reply(w, 200, n)
			} else {
				src, e := svc.Import(r.Context(), in)
				if e != nil {
					return e
				}
				reply(w, 201, src)
			}
			return nil
		}))
	}
	mux.HandleFunc("GET /api/v1/sources/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		src, e := store.Get[model.Source](r.Context(), svc.DB.Pool, "sources", r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 200, src)
		return nil
	}))
	mux.HandleFunc("PUT /api/v1/sources/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in service.Input
		if e := decode(w, r, &in); e != nil {
			return e
		}
		v, e := svc.EditSource(r.Context(), r.PathValue("id"), in)
		if e != nil {
			return e
		}
		reply(w, 200, v)
		return nil
	}))
	mux.HandleFunc("DELETE /api/v1/sources/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := svc.DeleteSource(r.Context(), r.PathValue("id")); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/revisions", handle(func(w http.ResponseWriter, r *http.Request) error {
		rs, e := store.List[model.Revision](r.Context(), svc.DB.Pool, "revisions")
		if e != nil {
			return e
		}
		out := []model.Revision{}
		for _, v := range rs {
			if v.SourceID == r.PathValue("id") {
				out = append(out, v)
			}
		}
		reply(w, 200, out)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/probes", handle(func(w http.ResponseWriter, r *http.Request) error {
		ps, e := store.List[model.Probe](r.Context(), svc.DB.Pool, "probes")
		if e != nil {
			return e
		}
		out := []model.Probe{}
		for _, v := range ps {
			if v.SourceID == r.PathValue("id") {
				out = append(out, v)
			}
		}
		reply(w, 200, out)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/revisions/{revision}/diff", handle(func(w http.ResponseWriter, r *http.Request) error {
		v, e := store.Get[model.Revision](r.Context(), svc.DB.Pool, "revisions", r.PathValue("revision"))
		if e != nil {
			return e
		}
		if v.SourceID != r.PathValue("id") {
			return store.ErrNotFound
		}
		reply(w, 200, v.Diff)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/revisions/{revision}/raw", handle(func(w http.ResponseWriter, r *http.Request) error {
		v, e := store.Get[model.Revision](r.Context(), svc.DB.Pool, "revisions", r.PathValue("revision"))
		if e != nil {
			return e
		}
		if v.SourceID != r.PathValue("id") {
			return store.ErrNotFound
		}
		b, e := svc.Vault.ReadSnapshot(v.Hash)
		if e != nil {
			return e
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=snapshot.txt")
		_, e = w.Write(b)
		return e
	}))
	mux.HandleFunc("POST /api/v1/sources/{id}/{action}", handle(func(w http.ResponseWriter, r *http.Request) error {
		id, a := r.PathValue("id"), r.PathValue("action")
		if a == "sync" || a == "probe" {
			job, e := svc.Enqueue(r.Context(), "source."+a, id)
			if e != nil {
				return e
			}
			reply(w, 202, map[string]string{"jobId": job})
			return nil
		}
		var in struct {
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if e := svc.SourceAction(r.Context(), id, a, in.Revision); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	for _, method := range []string{"POST", "PUT"} {
		path := "/api/v1/catalogs"
		if method == "PUT" {
			path += "/{id}"
		}
		mux.HandleFunc(method+" "+path, handle(func(w http.ResponseWriter, r *http.Request) error {
			var in model.Catalog
			if e := decode(w, r, &in); e != nil {
				return e
			}
			v, e := svc.SaveCatalog(r.Context(), r.PathValue("id"), in)
			if e != nil {
				return e
			}
			reply(w, 200, v)
			return nil
		}))
	}
	mux.HandleFunc("POST /api/v1/catalogs/{id}/sync", handle(func(w http.ResponseWriter, r *http.Request) error {
		id, e := svc.Enqueue(r.Context(), "catalog.sync", r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 202, map[string]string{"jobId": id})
		return nil
	}))
	mux.HandleFunc("POST /api/v1/candidates/{id}/{action}", handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := svc.CandidateAction(r.Context(), r.PathValue("id"), r.PathValue("action")); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	for _, method := range []string{"POST", "PUT"} {
		path := "/api/v1/source-sets"
		if method == "PUT" {
			path += "/{id}"
		}
		mux.HandleFunc(method+" "+path, handle(func(w http.ResponseWriter, r *http.Request) error {
			var in model.SourceSet
			if e := decode(w, r, &in); e != nil {
				return e
			}
			v, e := svc.SaveSet(r.Context(), r.PathValue("id"), in)
			if e != nil {
				return e
			}
			reply(w, 200, v)
			return nil
		}))
	}
	mux.HandleFunc("POST /api/v1/source-sets/{id}/preview", handle(func(w http.ResponseWriter, r *http.Request) error {
		p, e := svc.PreviewPublication(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		// Report counts/sizes without copying rule bodies or a non-existent publication ID.
		artifacts := map[string]any{}
		for path, artifact := range p.Artifacts {
			artifacts[path] = map[string]any{"contentType": artifact.ContentType, "bytes": len(artifact.Body)}
		}
		reply(w, 200, map[string]any{"setId": p.SetID, "sourceRevisions": p.SourceRevisions, "exclusions": p.Exclusions, "formatWarnings": p.FormatWarnings, "artifacts": artifacts})
		return nil
	}))
	mux.HandleFunc("POST /api/v1/source-sets/{id}/publish", handle(func(w http.ResponseWriter, r *http.Request) error {
		p, e := svc.Publish(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 201, p)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/publications/{id}/rollback", handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := svc.RollbackPublication(r.Context(), r.PathValue("id")); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	mux.HandleFunc("GET /api/v1/publications/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		p, e := store.Get[model.Publication](r.Context(), svc.DB.Pool, "publications", r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 200, p)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/bindings", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in model.Binding
		if e := decode(w, r, &in); e != nil {
			return e
		}
		b, token, e := svc.CreateBinding(r.Context(), in)
		if e != nil {
			return e
		}
		reply(w, 201, map[string]any{"binding": b, "token": token, "baseUrl": strings.TrimRight(s.PublicURL, "/") + "/p/" + token})
		return nil
	}))
	mux.HandleFunc("POST /api/v1/bindings/{id}/{action}", handle(func(w http.ResponseWriter, r *http.Request) error {
		token, e := svc.BindingAction(r.Context(), r.PathValue("id"), r.PathValue("action"))
		if e != nil {
			return e
		}
		reply(w, 200, map[string]string{"token": token, "baseUrl": strings.TrimRight(s.PublicURL, "/") + "/p/" + token})
		return nil
	}))
	for _, method := range []string{"POST", "PUT"} {
		path := "/api/v1/runtimes"
		if method == "PUT" {
			path += "/{id}"
		}
		mux.HandleFunc(method+" "+path, handle(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				model.Runtime
				Headers map[string]string `json:"headers"`
			}
			if e := decode(w, r, &in); e != nil {
				return e
			}
			v, e := svc.SaveRuntime(r.Context(), r.PathValue("id"), in.Runtime, in.Headers)
			if e != nil {
				return e
			}
			reply(w, 200, v)
			return nil
		}))
	}
	mux.HandleFunc("POST /api/v1/runtimes/{id}/{action}", handle(func(w http.ResponseWriter, r *http.Request) error {
		a := r.PathValue("action")
		if a != "test" && a != "sync" {
			return store.ErrNotFound
		}
		id, e := svc.Enqueue(r.Context(), "runtime."+a, r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 202, map[string]string{"jobId": id})
		return nil
	}))
	mux.HandleFunc("POST /api/v1/secrets/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Headers map[string]string `json:"headers"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if e := svc.SetHeaders(r.Context(), r.PathValue("id"), in.Headers); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	mux.HandleFunc("GET /api/v1/secrets/{id}", handle(func(w http.ResponseWriter, r *http.Request) error {
		h, e := svc.Headers(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		keys := []string{}
		for k := range h {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		reply(w, 200, map[string]any{"configured": len(keys) > 0, "headerNames": keys})
		return nil
	}))
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) { reply(w, 404, map[string]string{"error": "not found"}) })
}
func (s *Server) publication(w http.ResponseWriter, r *http.Request) {
	token, path := r.PathValue("token"), r.PathValue("path")
	pub := ""
	prefix := "/p/" + token
	if strings.HasPrefix(path, "v/") {
		parts := strings.SplitN(path, "/", 3)
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		pub = parts[1]
		path = parts[2]
		prefix += "/v/" + pub
	}
	a, e := s.Service.Resolve(r.Context(), token, pub, path)
	if e != nil {
		if s.limited(r) {
			reply(w, 429, map[string]string{"error": "too many invalid subscription requests"})
			return
		}
		http.NotFound(w, r)
		return
	}
	base := strings.TrimRight(s.PublicURL, "/") + prefix
	body := strings.ReplaceAll(a.Body, service.BasePlaceholder, base)
	etag := `"` + security.Hash([]byte(body)) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache, max-age=0")
	w.Header().Set("Content-Type", a.ContentType)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	_, _ = io.WriteString(w, body)
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	if strings.Contains(clean, "/.") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.WebDir, clean)
	if info, e := os.Stat(path); e == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	if strings.Contains(filepath.Base(clean), ".") {
		http.NotFound(w, r)
		return
	}
	index := filepath.Join(s.WebDir, "index.html")
	if _, e := os.Stat(index); e != nil {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Shadow Relay API is ready. Build web/ to enable the console.")
		return
	}
	http.ServeFile(w, r, index)
}
