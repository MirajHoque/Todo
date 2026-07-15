// Package handlers contains all HTTP handlers for the todo application.
//
// Handler responsibilities:
//   - Parse and validate the HTTP request
//   - Call the appropriate service / agent
//   - Write the HTTP response
//
// Handlers do NOT contain business logic. They are a thin translation layer
// between HTTP and your domain code.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
	"github.com/MirajHoque/todo-app/internal/models"
)

// ---- helpers ----------------------------------------------------------------

// writeJSON encodes v as JSON and writes it with the given status code.
// Every handler uses this — it ensures we always set Content-Type correctly.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// At this point we've already written the header, so we can't change
		// the status. Just log it.
		slog.Error("writeJSON encode failed", slog.String("error", err.Error()))
	}
}

// writeError writes a standard error envelope.
// Includes the request_id so the caller can correlate with Loki logs.
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeJSON(w, status, models.ErrorResponse{
		Error:     msg,
		RequestID: middleware.GetRequestID(r.Context()),
	})
}

// ---- Health handler ---------------------------------------------------------

// HealthResponse is what GET /health returns.
type HealthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Timestamp string `json:"timestamp"`
}

// Health handles GET /health.
// This endpoint is called by:
//   - Your load balancer / container orchestrator (liveness probe)
//   - Grafana Alloy health checks
//   - You, to verify the server started correctly
//
// It intentionally does NOT check the database here.
// A separate /health/ready endpoint (added in step 3) will check DB connectivity.
func Health(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	log.Debug("health check called")

	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		Service:   "todo-app",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
