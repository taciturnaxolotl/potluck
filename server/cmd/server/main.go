// Command server is the potluck backend.
//
// It owns auth, conversations, messages, the ledger, and the SSE stream
// tee. The Cloudflare Worker in /web is a dumb static-asset server plus
// /api/* proxy that forwards to here.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"charm.land/log/v2"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/taciturnaxolotl/potluck/internal/api/middleware"
	"github.com/taciturnaxolotl/potluck/internal/api/v1"
	"github.com/taciturnaxolotl/potluck/internal/api/web"
	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/config"
	"github.com/taciturnaxolotl/potluck/internal/hca"
	"github.com/taciturnaxolotl/potluck/internal/ledger"
	"github.com/taciturnaxolotl/potluck/internal/migrations"
	"github.com/taciturnaxolotl/potluck/internal/money"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/provider/registry"
	"github.com/taciturnaxolotl/potluck/internal/store"
	"github.com/taciturnaxolotl/potluck/internal/stream"

	_ "modernc.org/sqlite"

	// Register provider capabilities via init().
	_ "github.com/taciturnaxolotl/potluck/internal/pool/providers/generic"
	_ "github.com/taciturnaxolotl/potluck/internal/pool/providers/nvidia"
	_ "github.com/taciturnaxolotl/potluck/internal/pool/providers/omlx"
	_ "github.com/taciturnaxolotl/potluck/internal/pool/providers/pioneer"
)

// Information set at build time via -ldflags.
var (
	Version    = "dev"
	CommitSHA  = ""
	CommitDate = ""
)

func main() {
	autoMigrate := flag.Bool("auto-migrate", false, "run pending migrations on boot")
	flag.Parse()

	cfg := config.MustGet()

	switch {
	case cfg.IsProduction():
		log.SetLevel(log.InfoLevel)
		log.SetFormatter(log.JSONFormatter)
	case cfg.IsLocal():
		log.SetReportCaller(true)
		log.SetLevel(log.DebugLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}

	log.Info("Potluck",
		"version", Version,
		"commit", CommitSHA,
		"commit_date", CommitDate,
		"env", cfg.Environment,
	)

	if dir := filepath.Dir(cfg.DatabaseURL); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal("create db directory", "err", err)
		}
	}

	db, err := sql.Open("sqlite", cfg.DatabaseURL+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal("open database", "err", err)
	}
	defer db.Close()

	if *autoMigrate {
		log.Info("Running migrations")
		if err := migrations.Run(db); err != nil {
			log.Fatal("migrations failed", "err", err)
		}
	}

	q := store.New(db)
	authSvc := auth.New(q, time.Duration(cfg.SessionTTL)*time.Second)
	ledg := ledger.New(q,
		money.Micros(cfg.Spend.MinBalanceToStartMicros),
		cfg.Spend.MaxConcurrentStreams,
	)
	hub := stream.NewHub(q)

	// Build multi-provider registry from DB. Falls back to a pioneer-only
	// registry if the providers table hasn't been migrated yet.
	reg, err := registry.LoadFromDB(context.Background(), q)
	if err != nil {
		log.Warn("failed to load providers from DB, using pioneer-only fallback", "err", err)
		configs := []registry.ProviderConfig{
			{ID: "pioneer", Type: registry.TypeOpenAICompat, Name: "Pioneer", BaseURL: cfg.Pioneer.BaseURL},
		}
		reg = registry.New(configs)
	}
	log.Info("provider registry loaded", "providers", len(reg.List()))

	keyPool, err := pool.New(q, cfg.PoolKeySecret)
	if err != nil {
		log.Fatal("pool: init failed", "err", err)
	}

	// Start the background reconciler: syncs pioneer billing data every 10 min.
	reconcilerCtx, reconcilerCancel := context.WithCancel(context.Background())
	defer reconcilerCancel()
	reconciler := pool.NewReconciler(q, keyPool.Decrypt, log.Default())
	go reconciler.Run(reconcilerCtx)

	// Start the models refresher: updates models_catalog hourly.
	go pool.NewModelsRefresher(q, keyPool, reg, log.Default()).Run(reconcilerCtx)

	// Start the allocation recomputer: every N minutes, refresh per-user
	// daily allowances using smartAllocate so behavior changes (light user
	// spiking, new user joining) propagate without manual intervention.
	allocSrv := &web.Server{Q: q}
	go runAllocationRecomputer(reconcilerCtx, allocSrv, cfg.Spend.RecomputeIntervalSeconds)

	// Hack Club Auth client. Nil when unconfigured; the handlers degrade
	// gracefully and return 503 with a friendly note.
	var hcaClient *hca.Client
	if cfg.HCA.Valid() {
		hcaClient = hca.New(cfg.HCA.BaseURL, cfg.HCA.ClientID, cfg.HCA.ClientSecret, cfg.HCA.RedirectURL, cfg.HCA.Scopes)
		log.Info("Hack Club Auth wired", "redirect", cfg.HCA.RedirectURL)
	} else {
		log.Warn("Hack Club Auth not configured — /auth/login will return 503")
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(requestLogger)
	r.Use(chimw.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// /api/stats — public splash data; no auth, no PII.
	r.Get("/api/stats", publicStatsHandler(q))

	// Hack Club Auth: standard OAuth authorization-code flow.
	sessionTTL := time.Duration(cfg.SessionTTL) * time.Second
	r.Get("/auth/login", hcaLoginHandler(hcaClient, cfg.IsProduction()))
	r.Get("/auth/callback", hcaCallbackHandler(hcaClient, q, authSvc, sessionTTL, cfg.IsProduction(), cfg.WaitlistEnabled))
	r.Post("/auth/logout", hcaLogoutHandler(authSvc, cfg.IsProduction()))

	apiSrv := &web.Server{
		Q:        q,
		Auth:     authSvc,
		Ledger:   ledg,
		Hub:      hub,
		Pool:     keyPool,
		Registry: reg,
	}
	v1Srv := &v1.Server{
		Q:        q,
		Auth:     authSvc,
		Ledger:   ledg,
		Pool:     keyPool,
		Registry: reg,
	}

	// /api/models — public model catalog; no user-specific data.
	r.Get("/api/models", apiSrv.HandleListModelsPublic)

	// /api/* — cookie-authenticated, internal surface.
	r.Route("/api", func(r chi.Router) {
		r.Use(authSvc.Middleware)
		r.Use(auth.Require)
		r.Use(middleware.RequireActive(web.WriteErr))
		r.Use(middleware.RateLimit(20, 40, web.WriteErr))
		apiSrv.Mount(r)
	})

	// /v1/* — bearer-authenticated, OpenAI-compatible public surface.
	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.BearerAuth(q, v1.WriteError))
		r.Use(middleware.RequireActive(v1.WriteError))
		r.Use(middleware.RateLimit(10, 20, v1.WriteError))
		v1Srv.Mount(r)
	})

	srv := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("Starting HTTP server", "addr", cfg.HTTPListen)
		errc <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Info("Received signal, shutting down", "signal", sig)
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("HTTP server crashed", "err", err)
		}
	}

	log.Info("Shutting down HTTP server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	log.Info("Bye")
}

// requestLogger logs each request with method, path, status, and duration.
//
// Lines are intentionally short — the HTTP middleware is the noisiest log
// source in the system, so noise control matters. Health checks log at
// Debug, everything else at Info, errors at Warn (4xx) or Error (5xx).
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		fields := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"dur", time.Since(start).Round(time.Microsecond),
			"req_id", chimw.GetReqID(r.Context()),
		}
		switch {
		case r.URL.Path == "/healthz":
			log.Debug("HTTP", fields...)
		case ww.Status() >= 500:
			log.Error("HTTP", fields...)
		case ww.Status() >= 400:
			log.Warn("HTTP", fields...)
		default:
			log.Info("HTTP", fields...)
		}
	})
}
