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

	"github.com/MirajHoque/todo-app/internal/agents/memory"
	"github.com/MirajHoque/todo-app/internal/auth"
	"github.com/MirajHoque/todo-app/internal/config"
	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/handlers"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
	// <pathOftheModule>/<packageWantToImport>
)

func main() {
	// ── 1. Config ─────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ── 2. Logger ──────────────────────────────────────────────────────────
	log := logger.New(cfg.LogFormat, cfg.LogLevel, cfg.Env)
	log.Info("starting todo-app",
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.Addr()),
	)

	// ── 3. Database ────────────────────────────────────────────────────────
	ctx := context.Background()

	database, err := db.Connect(ctx, cfg, log)
	if err != nil {
		log.Error("failed to connect to database",
			slog.String(logger.FieldError, err.Error()),
		)
		os.Exit(1)
	}
	defer database.Close()

	// ── 4. Migrations ──────────────────────────────────────────────────────
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	migrationsDir := filepath.Join(projectRoot, "migrations")

	if err := database.Migrate(ctx, migrationsDir, log); err != nil {
		log.Error("failed to run migrations",
			slog.String(logger.FieldError, err.Error()),
		)
		os.Exit(1)
	}

	// ── 5. Services ────────────────────────────────────────────────────────
	// JWT service — creates and validates tokens
	jwtService := auth.NewService(cfg.JWTSecret, cfg.JWTExpiry)

	// Agent memory manager — one memory store per (user, agent) pair
	memoryManager := memory.NewManager(10)

	// Repositories — database access per table
	userRepo := db.NewUserRepository(database)

	// Handlers — HTTP layer
	authHandler := handlers.NewAuthHandler(userRepo, jwtService, memoryManager)

	// ── 6. Router ──────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestLog)
	r.Use(chimiddleware.StripSlashes)

	// Public routes
	r.Get("/health", handlers.Health)
	r.Get("/health/ready", handlers.NewHealthReady(database))

	r.Route("/api/v1", func(r chi.Router) {

		// Auth — public
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)

		// Protected — JWT middleware runs first for every route in this group
		r.Group(func(r chi.Router) {
			r.Use(jwtService.Middleware)

			r.Post("/auth/logout", authHandler.Logout)

			// Todo routes added in step 4
		})
	})

	// ── 7. HTTP server with graceful shutdown ──────────────────────────────
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
