package httpapi

import (
	"net/http"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/service"
)

func zipReply(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="shadow-relay-hub-plugins.zip"`)
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

func (s *Server) workshopRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/sources/{id}/content", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Content           string `json:"content"`
			ExpectedUpdatedAt string `json:"expectedUpdatedAt"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		v, e := s.Service.StageContent(r.Context(), r.PathValue("id"), in.Content, in.ExpectedUpdatedAt)
		if e != nil {
			return e
		}
		reply(w, 200, v)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/book-tools/convert", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in service.Input
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if in.Name == "" {
			in.Name = "书源兼容检查"
		}
		n, _, e := s.Service.Preview(r.Context(), in)
		if e != nil {
			return e
		}
		mode := ""
		if in.HubProxyMode != nil {
			mode = *in.HubProxyMode
		}
		report := service.ConvertBookPreview(n, mode)
		reply(w, 200, report)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/book-tools/scaffold", handle(func(w http.ResponseWriter, r *http.Request) error {
		var in bookplugin.Recipe
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if e := bookplugin.Validate(&in); e != nil {
			return e
		}
		b, e := service.ScaffoldBook(in)
		if e != nil {
			return e
		}
		zipReply(w, b)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/book-plugins", handle(func(w http.ResponseWriter, r *http.Request) error {
		report, _, e := s.Service.BookReport(r.Context(), r.PathValue("id"), r.URL.Query().Get("revision"))
		if e != nil {
			return e
		}
		reply(w, 200, report)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/sources/{id}/hub.zip", handle(func(w http.ResponseWriter, r *http.Request) error {
		report, updated, e := s.Service.BookReport(r.Context(), r.PathValue("id"), r.URL.Query().Get("revision"))
		if e != nil {
			return e
		}
		b, e := bookplugin.Archive(report, updated)
		if e != nil {
			return e
		}
		zipReply(w, b)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/runtimes/{id}/hub-plugins", handle(func(w http.ResponseWriter, r *http.Request) error {
		v, e := s.Service.HubPlugins(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		reply(w, 200, v)
		return nil
	}))
	mux.HandleFunc("POST /api/v1/runtimes/{id}/hub-reload", handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := s.Service.ReloadHub(r.Context(), r.PathValue("id")); e != nil {
			return e
		}
		reply(w, 200, map[string]bool{"reloaded": true})
		return nil
	}))
	mux.HandleFunc("GET /api/v1/recipes", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, service.ReferenceRecipes()) })
}
