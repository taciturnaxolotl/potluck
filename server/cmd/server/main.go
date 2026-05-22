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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/taciturnaxolotl/potluck/internal/api"
	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/config"
	"github.com/taciturnaxolotl/potluck/internal/ledger"
	"github.com/taciturnaxolotl/potluck/internal/migrations"
	"github.com/taciturnaxolotl/potluck/internal/money"
	"github.com/taciturnaxolotl/potluck/internal/store"
	"github.com/taciturnaxolotl/potluck/internal/stream"

	_ "modernc.org/sqlite"
)

func main() {
	autoMigrate := flag.Bool("auto-migrate", false, "run pending migrations on boot")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.MustGet()

	if dir := filepath.Dir(cfg.DatabaseURL); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Error("mkdir db dir", "err", err)
			os.Exit(2)
		}
	}

	db, err := sql.Open("sqlite", cfg.DatabaseURL+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(2)
	}
	defer db.Close()

	if *autoMigrate {
		if err := migrations.Run(db); err != nil {
			logger.Error("migrate", "err", err)
			os.Exit(2)
		}
	}

	q := store.New(db)
	authSvc := auth.New(q, time.Duration(cfg.SessionTTL)*time.Second)
	ledg := ledger.New(q,
		money.Micros(cfg.Spend.MinBalanceToStartMicros),
		cfg.Spend.MaxConcurrentStreams,
	)
	hub := stream.NewHub(q)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(slogRequest(logger))
	r.Use(middleware.Recoverer)
	r.Use(authSvc.Middleware)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// Local-only login: trade an email for a session cookie. Real auth
	// lives behind a real provider — see design/security.md.
	if cfg.IsLocal() {
		r.Post("/api/dev/login", func(w http.ResponseWriter, r *http.Request) {
			email := r.URL.Query().Get("email")
			if email == "" {
				http.Error(w, "missing email", 400)
				return
			}
			u, err := q.GetUserByEmail(r.Context(), email)
			if errors.Is(err, sql.ErrNoRows) {
				u, err = q.CreateUser(r.Context(), store.CreateUserParams{
					ID:          uuid.NewString(),
					Email:       email,
					DisplayName: email,
					CreatedAt:   time.Now().Unix(),
				})
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			tok, err := authSvc.IssueSession(r.Context(), u.ID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     auth.CookieName,
				Value:    tok,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Expires:  time.Now().Add(time.Duration(cfg.SessionTTL) * time.Second),
			})
			_, _ = w.Write([]byte("ok"))
		})
	}

	apiSrv := &api.Server{
		Q:       q,
		Auth:    authSvc,
		Ledger:  ledg,
		Hub:     hub,
		Limiter: rate.NewLimiter(rate.Limit(50), 100),
	}

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.Require)
			apiSrv.Mount(r)
		})
	})

	srv := &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.HTTPListen, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// slogRequest logs each request with method, path, status, and duration.
func slogRequest(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			l.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur_ms", time.Since(start).Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
