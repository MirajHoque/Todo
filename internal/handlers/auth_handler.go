// auth_handler.go — HTTP handlers for register, login, logout.
//
// Each handler follows the same pattern:
//  1. Decode + validate the request body
//  2. Call the repository or service
//  3. Log the outcome (success or failure) with structured fields
//  4. Write the JSON response
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/MirajHoque/todo-app/internal/auth"
	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
	"github.com/MirajHoque/todo-app/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler holds the dependencies for auth endpoints.
// Dependencies are injected — the handler doesn't create them.
type AuthHandler struct {
	users      *db.UserRepository
	jwtService *auth.Service
	memory     interface{ ClearUser(string) } // agent memory manager
}

// NewAuthHandler creates an AuthHandler with its dependencies.
func NewAuthHandler(
	users *db.UserRepository,
	jwtService *auth.Service,
	memory interface{ ClearUser(string) },
) *AuthHandler {
	return &AuthHandler{
		users:      users,
		jwtService: jwtService,
		memory:     memory,
	}
}

// ── Register ─────────────────────────────────────────────────────────────────

// Register handles POST /api/v1/auth/register
//
// Request:  {"email": "ali@example.com", "password": "secret123"}
// Response: {"token": "eyJ...", "user": {"id": "...", "email": "..."}}
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// ── 1. Decode request body ────────────────────────────────────────────
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	// ── 2. Validate input ─────────────────────────────────────────────────
	if err := req.Validate(); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// ── 3. Hash the password ──────────────────────────────────────────────
	// bcrypt.GenerateFromPassword is deliberately slow (cost=12).
	// This makes brute-force attacks impractical even if the DB is stolen.
	// We never store the raw password — only the hash.
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to hash password",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "register_hash_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	// ── 4. Create user in database ────────────────────────────────────────
	user, err := h.users.CreateUser(r.Context(), req.Email, string(hash))
	if err != nil {
		if errors.Is(err, db.ErrEmailTaken) {
			writeError(w, r, http.StatusConflict, "email already registered")
			return
		}
		log.Error("failed to create user",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "register_db_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	// ── 5. Issue JWT token ────────────────────────────────────────────────
	token, err := h.jwtService.CreateToken(user.ID)
	if err != nil {
		log.Error("failed to create token",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "register_token_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("user registered",
		slog.String(logger.FieldUserID, user.ID),
		slog.String(logger.FieldAction, "register_ok"),
	)

	writeJSON(w, http.StatusCreated, models.AuthResponse{
		Token: token,
		User:  user,
	})
}

// ── Login ─────────────────────────────────────────────────────────────────────

// Login handles POST /api/v1/auth/login
//
// Request:  {"email": "ali@example.com", "password": "secret123"}
// Response: {"token": "eyJ...", "user": {"id": "...", "email": "..."}}
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// ── 1. Decode + validate ──────────────────────────────────────────────
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, r, http.StatusBadRequest, "email and password are required")
		return
	}

	// ── 2. Look up user by email ──────────────────────────────────────────
	user, err := h.users.FindByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			// IMPORTANT: return the same error message whether the email
			// doesn't exist OR the password is wrong. This prevents
			// attackers from enumerating valid email addresses.
			writeError(w, r, http.StatusUnauthorized, "invalid email or password")
			return
		}
		log.Error("failed to find user",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "login_db_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	// ── 3. Verify password ────────────────────────────────────────────────
	// bcrypt.CompareHashAndPassword re-hashes the input and compares.
	// Returns nil if match, error if wrong.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		log.Warn("invalid password attempt",
			slog.String(logger.FieldUserID, user.ID),
			slog.String(logger.FieldAction, "login_wrong_password"),
		)
		// Same message as "user not found" — prevents email enumeration.
		writeError(w, r, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// ── 4. Issue JWT token ────────────────────────────────────────────────
	token, err := h.jwtService.CreateToken(user.ID)
	if err != nil {
		log.Error("failed to create token",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "login_token_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("user logged in",
		slog.String(logger.FieldUserID, user.ID),
		slog.String(logger.FieldAction, "login_ok"),
	)

	// Clear password hash before sending — extra safety beyond json:"-"
	user.PasswordHash = ""

	writeJSON(w, http.StatusOK, models.AuthResponse{
		Token: token,
		User:  user,
	})
}

// ── Logout ────────────────────────────────────────────────────────────────────

// Logout handles POST /api/v1/auth/logout
//
// JWT is stateless — we cannot "invalidate" a token on the server.
// What we CAN do is clear the agent memory for this user so the
// next session starts completely fresh.
//
// The client is responsible for deleting the token on their side.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())

	// Clear all agent memory for this user.
	// Next login starts with a clean conversation history.
	h.memory.ClearUser(userID)

	log.Info("user logged out",
		slog.String(logger.FieldUserID, userID),
		slog.String(logger.FieldAction, "logout_ok"),
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "logged out successfully",
	})
}
