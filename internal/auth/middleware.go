// auth_middleware.go — validates JWT on every protected route.
//
// HOW IT WORKS
// ============
// This middleware sits in front of protected route groups.
// Every request to a protected route passes through here first.
//
// 1. Read Authorization header: "Bearer <token>"
// 2. Extract the token string
// 3. Validate it using the JWT service
// 4. Put user_id into context
// 5. Call the next handler
//
// If any step fails → return 401 immediately. Handler never runs.
package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
)

// Middleware returns an HTTP middleware that validates JWT tokens.
// Use it to protect route groups in main.go:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(authService.Middleware)
//	    r.Post("/todos", handlers.CreateTodo)
//	})
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.FromContext(r.Context())

		// ── 1. Read the Authorization header ─────────────────────────────
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			log.Warn("missing authorization header",
				slog.String(logger.FieldAction, "auth_missing_header"),
			)
			writeUnauthorized(w, "missing authorization header")
			return
		}

		// ── 2. Extract the token ──────────────────────────────────────────
		// Header format: "Bearer <token>"
		// strings.Cut splits on the first space.
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			log.Warn("malformed authorization header",
				slog.String(logger.FieldAction, "auth_malformed_header"),
			)
			writeUnauthorized(w, "authorization header must be: Bearer <token>")
			return
		}
		tokenString := parts[1]

		// ── 3. Validate the token ─────────────────────────────────────────
		claims, err := s.ValidateToken(tokenString)
		if err != nil {
			log.Warn("invalid token",
				slog.String(logger.FieldAction, "auth_invalid_token"),
				slog.String(logger.FieldError, err.Error()),
			)
			writeUnauthorized(w, err.Error())
			return
		}

		// ── 4. Store user_id in context ───────────────────────────────────
		// From here, any handler can call middleware.GetUserID(ctx)
		// to know who is making the request.
		ctx := middleware.SetUserID(r.Context(), claims.UserID)

		// Also attach user_id to the logger so every log line in this
		// request automatically includes it — visible in Loki/Grafana.
		log = log.With(slog.String(logger.FieldUserID, claims.UserID))
		ctx = logger.WithContext(ctx, log)

		log.Debug("token validated",
			slog.String(logger.FieldAction, "auth_ok"),
		)

		// ── 5. Call the next handler ──────────────────────────────────────
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeUnauthorized writes a 401 response with a JSON error body.
func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}
