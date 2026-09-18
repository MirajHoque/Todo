// Package db manages the PostgreSQL connection pool and database operations.
//
// WHY pgx INSTEAD OF database/sql?
// pgx is a PostgreSQL-specific driver that gives us:
//   - Native support for PostgreSQL types (UUID, TIMESTAMPTZ, arrays)
//   - Better performance than database/sql
//   - pgxpool for connection pooling (essential for web apps)
//   - Cleaner error handling with pgconn.PgError
//
// CONNECTION POOL
// A pool keeps N connections open and ready.
// Without a pool, every request opens a new TCP connection to Azure
// PostgreSQL — expensive (100-300ms each time).
// With a pool, connections are reused — near zero overhead.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MirajHoque/todo-app/internal/config"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool.Pool so we can add methods to it.
// All database operations in the app go through this type.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect creates a connection pool to Azure PostgreSQL.
// Call this once at startup in main.go.
// Returns an error if the database is unreachable — the app should not start.
func Connect(ctx context.Context, cfg *config.Config, log *slog.Logger) (*DB, error) {
	log.Info("connecting to database",
		slog.String(logger.FieldAction, "db_connect"),
	)

	// Parse the DATABASE_URL into a pgx pool config.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	// Connection pool settings — tune these for your workload.
	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdle

	// How long to wait for a connection from the pool before giving up.
	poolCfg.MaxConnLifetime = 1 * time.Hour

	// Create the pool. This does NOT open connections yet —
	// connections are opened lazily on first use.
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	// Ping verifies we can actually reach the database.
	// This is the moment we find out if DATABASE_URL is wrong.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping failed: %w", err)
	}

	log.Info("database connected",
		slog.String(logger.FieldAction, "db_connected"),
		slog.Int("max_conns", int(cfg.DBMaxConns)),
		slog.Int("min_conns", int(cfg.DBMinConns)),
	)

	return &DB{Pool: pool}, nil
}

// Close shuts down the connection pool cleanly.
// Call this in main.go during graceful shutdown.
func (db *DB) Close() {
	db.Pool.Close()
}

// Ping checks if the database is still reachable.
// Used by the /health/ready endpoint.
func (db *DB) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return db.Pool.Ping(pingCtx)
}

// Stats returns connection pool statistics for monitoring.
// These are logged by the health endpoint so you can see them in Grafana.
func (db *DB) Stats() map[string]any {
	stats := db.Pool.Stat()
	return map[string]any{
		"total_conns":    stats.TotalConns(),
		"idle_conns":     stats.IdleConns(),
		"acquired_conns": stats.AcquiredConns(),
		"max_conns":      stats.MaxConns(),
	}
}
