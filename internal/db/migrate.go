// migrate.go runs SQL migration files in order.
//
// WHY WRITE OUR OWN INSTEAD OF USING A LIBRARY?
// Libraries like golang-migrate are great for large projects.
// For learning, writing our own teaches you exactly what migrations do:
//  1. Track which migrations have already run (in a DB table)
//  2. Run any new ones in order
//  3. Never run the same migration twice
//
// HOW IT WORKS
// We create a "schema_migrations" table in the database.
// Each migration file is recorded there after it runs.
// On next startup, we skip files already in that table.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MirajHoque/todo-app/internal/logger"
)

// Migrate runs all SQL files in the migrations/ directory that have not
// been run yet. Files are run in alphabetical order — which matches the
// 001_, 002_ numbering convention.
func (db *DB) Migrate(ctx context.Context, migrationsDir string, log *slog.Logger) error {
	// Create the tracking table if it doesn't exist yet.
	// This is idempotent — safe to run every startup.
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("migrate: create schema_migrations table: %w", err)
	}

	// Read all .sql files from the migrations directory.
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("migrate: read directory %q: %w", migrationsDir, err)
	}

	// Collect only .sql files and sort them alphabetically.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// Run each file that has not been applied yet.
	applied := 0
	for _, filename := range files {
		// Check if this migration has already been run.
		var exists bool
		err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`,
			filename,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("migrate: check %q: %w", filename, err)
		}
		if exists {
			log.Debug("migration already applied, skipping",
				slog.String("file", filename),
			)
			continue
		}

		// Read the SQL file content.
		path := filepath.Join(migrationsDir, filename)
		sql, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("migrate: read %q: %w", filename, err)
		}

		// Run the migration inside a transaction.
		// If it fails, the transaction rolls back — no half-applied migrations.
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("migrate: begin transaction for %q: %w", filename, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migrate: execute %q: %w", filename, err)
		}

		// Record that this migration was applied.
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`,
			filename,
		); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migrate: record %q: %w", filename, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("migrate: commit %q: %w", filename, err)
		}

		log.Info("migration applied",
			slog.String(logger.FieldAction, "migration_applied"),
			slog.String("file", filename),
		)
		applied++
	}

	if applied == 0 {
		log.Info("database schema is up to date")
	} else {
		log.Info("migrations complete",
			slog.String(logger.FieldAction, "migrations_complete"),
			slog.Int("applied", applied),
		)
	}

	return nil
}
