// Package db - user_repository.go
// Contains all database operations related to users.
//
// REPOSITORY PATTERN
// ==================
// A repository is a Go struct that owns all database queries
// for one specific table. Instead of writing SQL inside handlers,
// handlers call repository methods like:
//
//	repo.CreateUser(ctx, email, hash)
//	repo.FindByEmail(ctx, email)
//
// This keeps SQL in one place. If you change the DB schema,
// you only update the repository — not every handler.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/MirajHoque/todo-app/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// UserRepository handles all database operations for the users table.
type UserRepository struct {
	db *DB
}

// NewUserRepository creates a UserRepository.
// Called once in main.go and passed to the auth handler.
func NewUserRepository(db *DB) *UserRepository {
	return &UserRepository{db: db}
}

// CreateUser inserts a new user into the database.
// Returns the created User (with ID and timestamps filled by PostgreSQL).
// Returns ErrEmailTaken if the email is already registered.
func (r *UserRepository) CreateUser(ctx context.Context, email, passwordHash string) (*models.User, error) {
	user := &models.User{}

	err := r.db.Pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, created_at, updated_at
	`, email, passwordHash).Scan(
		&user.ID,
		&user.Email,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		// Check if this is a unique constraint violation on email.
		// pgconn.PgError is the PostgreSQL-specific error type from pgx.
		// Code "23505" = unique_violation in PostgreSQL.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("user repo: create user: %w", err)
	}

	return user, nil
}

// FindByEmail looks up a user by their email address.
// Returns ErrUserNotFound if no user exists with that email.
// Used during login to fetch the stored password hash for verification.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}

	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users
		WHERE email = $1
	`, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		// pgx returns pgx.ErrNoRows when SELECT finds nothing.
		// We convert it to our own error type so callers don't
		// need to import pgx just to check this condition.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("user repo: find by email: %w", err)
	}

	return user, nil
}

// FindByID looks up a user by their ID.
// Used by the JWT middleware to verify the user still exists.
func (r *UserRepository) FindByID(ctx context.Context, id string) (*models.User, error) {
	user := &models.User{}

	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, email, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id).Scan(
		&user.ID,
		&user.Email,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("user repo: find by id: %w", err)
	}

	return user, nil
}

// ---- Sentinel errors --------------------------------------------------------
// Sentinel errors are package-level error values that callers can check with
// errors.Is(). They describe domain conditions, not technical failures.
//
// WHY NOT JUST RETURN A STRING ERROR?
// Because callers need to distinguish "user not found" (return 404)
// from "database connection failed" (return 500). String comparison is
// fragile. errors.Is() is the correct Go pattern.

// ErrUserNotFound is returned when a user lookup finds no matching row.
var ErrUserNotFound = errors.New("user not found")

// ErrEmailTaken is returned when registering with an already-used email.
var ErrEmailTaken = errors.New("email already taken")
