// Package logger sets up structured JSON logging using Go's built-in log/slog.
//
// WHY slog?
//   - Built into Go 1.21+ — no external dependency
//   - Structured key-value pairs → Loki can index them as labels
//   - JSON format → Alloy ingests it automatically
//   - Zero-alloc in the hot path (slog.LogAttrs)
//
// Log fields we emit on EVERY line:
//   - time        (RFC3339Nano)
//   - level       (INFO, WARN, ERROR, DEBUG)
//   - msg         (human-readable summary)
//   - service     (always "todo-app")
//   - env         (development | production)
//   - request_id  (added by middleware per-request)
//   - user_id     (added by auth middleware when logged in)
//
// Loki can then filter by:
//
//	{service="todo-app"} | level="ERROR"
//	{service="todo-app"} | user_id="abc-123"
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// contextKey is unexported so only this package can set/get logger from context.
type contextKey struct{}

// Fields that are always present in every log line.
const (
	FieldService   = "service"
	FieldEnv       = "env"
	FieldRequestID = "request_id"
	FieldUserID    = "user_id"
	FieldMethod    = "method"
	FieldPath      = "path"
	FieldStatus    = "status"
	FieldDuration  = "duration_ms"
	FieldError     = "error"
	FieldAgent     = "agent"
	FieldAction    = "action"
)

// New creates a structured slog.Logger.
//
//   - format "json"  → JSONHandler  (use in production, Alloy ingests this)
//   - format "text"  → TextHandler  (use in development, human-readable)
//   - level controls minimum log level; below this level is silently dropped
func New(format, level, env string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug, // include file:line only in debug mode
	}

	var handler slog.Handler
	var w io.Writer = os.Stdout // always log to stdout; Alloy tails stdout or the file

	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	// Stamp every log line with service name and environment.
	// These become Loki labels — keep them low-cardinality.
	logger := slog.New(handler).With(
		slog.String(FieldService, "todo-app"),
		slog.String(FieldEnv, env),
	)

	// Also set as the default so any package using slog.Info() etc. benefits.
	slog.SetDefault(logger)

	return logger
}

// WithContext stores the logger in the request context.
// Middleware calls this so handlers can retrieve it with FromContext.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the logger stored in ctx.
// Returns the default slog logger if none was stored — never returns nil.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// With returns a child logger with extra fields pre-attached.
// Use this in agents and handlers to add domain-specific context:
//
//	log := logger.With(base, slog.String("agent", "auth"), slog.String("user_id", uid))
func With(base *slog.Logger, args ...any) *slog.Logger {
	return base.With(args...)
}
