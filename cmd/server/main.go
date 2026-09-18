package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/MirajHoque/todo-app/internal/config"
	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/handlers"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
	// <pathOftheModule>/<packeageWantToImport>
)

func main() {
	// ── 1. Config ────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ── 2. Logger ────────────────────────────────────────────────────────────
	log := logger.New(cfg.LogFormat, cfg.LogLevel, cfg.Env)

	log.Info("starting todo-app",
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.Addr()),
	)

	// ── 3. Database ──────────────────────────────────────────────────────────
	// Use a background context for startup — not tied to any request.
	ctx := context.Background()

	database, err := db.Connect(ctx, cfg, log)
	if err != nil {
		log.Error("failed to connect to database",
			slog.String(logger.FieldError, err.Error()),
		)
		os.Exit(1)
	}
	defer database.Close()

	// ── 4. Migrations ────────────────────────────────────────────────────────
	// Find the migrations/ directory relative to this source file.
	// This works whether you run with go run or a compiled binary.
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	migrationsDir := filepath.Join(projectRoot, "migrations")

	if err := database.Migrate(ctx, migrationsDir, log); err != nil {
		log.Error("failed to run migrations",
			slog.String(logger.FieldError, err.Error()),
		)
		os.Exit(1)
	}

	// ── 5. Router + middleware ────────────────────────────────────────────────
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestLog)
	r.Use(chimiddleware.StripSlashes)

	// Public routes
	r.Get("/health", handlers.Health)
	r.Get("/health/ready", handlers.NewHealthReady(database))

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromContext(r.Context())
			log.Info("ping called")
			w.Write([]byte(`{"message":"pong"}`))
		})
	})

	// ── 6. HTTP server with graceful shutdown ────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.Addr()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Error("server error", slog.String(logger.FieldError, err.Error()))
		os.Exit(1)
	case sig := <-quit:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", slog.String(logger.FieldError, err.Error()))
		os.Exit(1)
	}

	log.Info("server stopped cleanly")
}
