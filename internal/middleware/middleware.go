// Package middleware provides HTTP middleware for the todo app.
//
// Middleware chain (applied in order by main.go):
//  1. RequestID   — stamps every request with a unique ID
//  2. Logger      — attaches a child logger (with request_id) to context
//  3. RequestLog  — logs method, path, status, duration on every request
//  4. Recoverer   — catches panics, logs them, returns 500
//  5. Auth        — validates JWT, attaches user_id to context (protected routes only)
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/google/uuid"
)

// ---- context keys -----------------------------------------------------------

type requestIDKey struct{}
type userIDKey struct{}

// ---- RequestID --------------------------------------------------------------

// RequestID generates a UUID for every incoming request and:
//   - stores it in the request context
//   - echoes it back in the X-Request-ID response header
//
// If the caller already set X-Request-ID (e.g. a load balancer), we re-use
// that value so the ID stays consistent across systems.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from ctx. Returns "" if not set.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// ---- Logger middleware -------------------------------------------------------

// Logger attaches a child slog.Logger to the request context.
// The child carries request_id and user_id (if logged in) as permanent fields,
// so every log.Info / log.Error inside a handler automatically includes them.
//
// Call this AFTER RequestID and AFTER Auth so all fields are available.
func Logger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := GetRequestID(r.Context())
			userID := GetUserID(r.Context())

			// Build a per-request child logger with the fields that Loki will index.
			child := base.With(
				slog.String(logger.FieldRequestID, requestID),
			)
			if userID != "" {
				child = child.With(slog.String(logger.FieldUserID, userID))
			}

			ctx := logger.WithContext(r.Context(), child)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---- RequestLog middleware ---------------------------------------------------

// responseWriter wraps http.ResponseWriter to capture the status code written
// by the handler. We need this because http.ResponseWriter doesn't expose it.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Status() int {
	if rw.status == 0 {
		return http.StatusOK // WriteHeader was never called → implicit 200
	}
	return rw.status
}

// RequestLog logs one line per HTTP request with method, path, status, and
// duration. This is the access log that will appear in Loki.
//
// Example output (JSON):
//
//	{
//	  "time": "2024-01-15T10:30:00Z",
//	  "level": "INFO",
//	  "msg": "request completed",
//	  "service": "todo-app",
//	  "request_id": "550e8400-e29b-41d4-a716-446655440000",
//	  "method": "POST",
//	  "path": "/api/v1/todos",
//	  "status": 201,
//	  "duration_ms": 42
//	}
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := wrapResponseWriter(w)

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Milliseconds()
		log := logger.FromContext(r.Context())

		log.Info("request completed",
			slog.String(logger.FieldMethod, r.Method),
			slog.String(logger.FieldPath, r.URL.Path),
			slog.Int(logger.FieldStatus, wrapped.Status()),
			slog.Int64(logger.FieldDuration, duration),
		)
	})
}

// ---- Recoverer --------------------------------------------------------------

// Recoverer catches any panic in a handler, logs it as an ERROR with the
// request_id so you can find it in Grafana, then returns a 500.
// Without this, a panic kills the goroutine silently.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log := logger.FromContext(r.Context())
				log.Error("panic recovered",
					slog.Any(logger.FieldError, rec),
					slog.String(logger.FieldMethod, r.Method),
					slog.String(logger.FieldPath, r.URL.Path),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- UserID context helpers -------------------------------------------------
// These are used by the Auth middleware (step 4) and by the Logger middleware above.

// SetUserID stores a user ID in the context. Called by the Auth middleware.
func SetUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// GetUserID retrieves the user ID from the context. Returns "" if not authenticated.
func GetUserID(ctx context.Context) string {
	if id, ok := ctx.Value(userIDKey{}).(string); ok {
		return id
	}
	return ""
}
