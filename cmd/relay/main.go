package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/httpapi"
	"github.com/Cylunex/shadow-relay/internal/security"
	"github.com/Cylunex/shadow-relay/internal/service"
	"github.com/Cylunex/shadow-relay/internal/store"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "keygen" {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			panic(e)
		}
		fmt.Printf("RELAY_MASTER_KEY=%s\nRELAY_ADMIN_TOKEN=%s\n", base64.StdEncoding.EncodeToString(b), security.Token())
		return
	}
	if mode != "serve" && mode != "api" && mode != "worker" && mode != "migrate" {
		fmt.Fprintln(os.Stderr, "usage: relay [serve|api|worker|migrate|keygen]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("RELAY_DATABASE_URL")
	if dsn == "" {
		slog.Error("RELAY_DATABASE_URL is required")
		os.Exit(1)
	}
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	db, e := store.Open(connectCtx, dsn)
	cancel()
	if e != nil {
		slog.Error("database connection failed")
		os.Exit(1)
	}
	defer db.Pool.Close()
	if e = db.Migrate(ctx); e != nil {
		slog.Error("database migration failed")
		os.Exit(1)
	}
	if mode == "migrate" {
		slog.Info("migrations applied")
		return
	}
	vault, e := security.NewVault(os.Getenv("RELAY_MASTER_KEY"), env("RELAY_DATA_DIR", "data"))
	if e != nil {
		slog.Error("vault initialization failed", "error", e.Error())
		os.Exit(1)
	}
	f, e := fetch.New(os.Getenv("RELAY_TRUSTED_CIDRS"))
	if e != nil {
		slog.Error(e.Error())
		os.Exit(1)
	}
	svc := &service.Service{DB: db, Vault: vault, Fetch: f}
	workers, e := strconv.Atoi(env("RELAY_WORKERS", "2"))
	if e != nil || workers < 1 || workers > 16 {
		slog.Error("RELAY_WORKERS must be 1–16")
		os.Exit(1)
	}
	if mode == "worker" {
		svc.Run(ctx, workers)
		return
	}
	admin := os.Getenv("RELAY_ADMIN_TOKEN")
	if len(admin) < 32 || strings.HasPrefix(admin, "REPLACE_") {
		slog.Error("RELAY_ADMIN_TOKEN must be a random token of at least 32 characters")
		os.Exit(1)
	}
	public := env("RELAY_PUBLIC_URL", "http://localhost:8080")
	if e = security.SafeURL(public); e != nil {
		slog.Error("RELAY_PUBLIC_URL must be an HTTP(S) base URL")
		os.Exit(1)
	}
	publicURL, _ := url.Parse(public)
	basePath := strings.TrimRight(publicURL.Path, "/")
	if publicURL.RawQuery != "" || publicURL.ForceQuery || publicURL.Fragment != "" || publicURL.RawPath != "" || (basePath != "" && path.Clean(basePath) != basePath) {
		slog.Error("RELAY_PUBLIC_URL must have a clean path without query or fragment")
		os.Exit(1)
	}
	api := &httpapi.Server{Service: svc, AdminToken: admin, WebDir: env("RELAY_WEB_DIR", "web/dist"), PublicURL: public}
	srv := &http.Server{Addr: env("RELAY_LISTEN", "127.0.0.1:8080"), Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 40 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	workerDone := make(chan struct{})
	if mode == "serve" {
		go func() { defer close(workerDone); svc.Run(ctx, workers) }()
	} else {
		close(workerDone)
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("Shadow Relay started", "mode", mode)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		stop()
	}
	stop()
	<-workerDone
}
