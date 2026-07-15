// main.go is the entry point for the todo-app server.
//
// It does exactly 4 things in order:
//  1. Load config from environment variables
//  2. Set up structured logging (slog → JSON → Alloy → Loki → Grafana)
//  3. Build the HTTP router with middleware
//  4. Start the server with graceful shutdown
//
// Nothing else lives here. All real logic is in internal/.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/MirajHoque/todo-app/internal/config"
	"github.com/MirajHoque/todo-app/internal/handlers"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
)

func main() {
	// ── 1. Config ────────────────────────────────────────────────────────────
	// Load panics immediately if a required env var is missing.
	// This is intentional: we want a loud, obvious startup failure rather
	// than a silent misconfiguration discovered hours later in production.
	cfg, err := config.Load()
	if err != nil {
		// slog isn't set up yet, so use stderr directly.
		slog.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ── 2. Logger ────────────────────────────────────────────────────────────
	// After this line, every package that calls slog.Info() / slog.Error()
	// will automatically get JSON output with service and env fields attached.
	log := logger.New(cfg.LogFormat, cfg.LogLevel, cfg.Env)

	log.Info("starting todo-app",
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.Addr()),
		slog.String("log_format", cfg.LogFormat),
		slog.String("log_level", cfg.LogLevel),
	)

	// ── 3. Router + middleware ────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware — runs on EVERY request in this order:
	r.Use(middleware.RequestID)       // 1. stamp request_id
	r.Use(middleware.Logger(log))     // 2. attach child logger to context
	r.Use(middleware.Recoverer)       // 3. catch panics → 500
	r.Use(middleware.RequestLog)      // 4. log method/path/status/duration
	r.Use(chimiddleware.StripSlashes) // 5. /todos/ → /todos (cleanliness)

	// Public routes — no auth required.
	r.Get("/health", handlers.Health)

	// API v1 — all application routes live under /api/v1.
	// Auth, todos, and agent routes will be mounted here in later steps.
	r.Route("/api/v1", func(r chi.Router) {
		// placeholder — auth and todo routes added in steps 4 and 5
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromContext(r.Context())
			log.Info("ping called")
			w.Write([]byte(`{"message":"pong"}`))
		})
	})

	// ── 4. HTTP server with graceful shutdown ────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		// Use our structured logger for server-level errors (e.g. TLS errors).
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// Start serving in a goroutine so we can listen for shutdown signals below.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.Addr()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Block until we receive SIGINT (Ctrl+C) or SIGTERM (Docker/k8s stop).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Error("server error", slog.String(logger.FieldError, err.Error()))
		os.Exit(1)

	case sig := <-quit:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Graceful shutdown: give in-flight requests 10 seconds to complete.
	// After that, force close.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", slog.String(logger.FieldError, err.Error()))
		os.Exit(1)
	}

	log.Info("server stopped cleanly")
}
