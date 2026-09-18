package handlers

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/logger"
)

// HealthReady handles GET /health/ready
//
// TWO HEALTH ENDPOINTS — why?
//
//   GET /health       → "is the process alive?" (liveness probe)
//                       Returns 200 immediately. No DB check.
//                       If this fails, the process is dead — restart it.
//
//   GET /health/ready → "is the app ready to serve traffic?" (readiness probe)
//                       Checks DB connectivity.
//                       If this fails, take it out of the load balancer rotation
//                       but don't restart it — it might be a DB blip.
//
// Grafana Alloy and Kubernetes both use this pattern.
// Your Loki setup can alert on readiness failures.

type ReadyResponse struct {
	Status    string         `json:"status"`
	Timestamp string         `json:"timestamp"`
	Database  DatabaseStatus `json:"database"`
}

type DatabaseStatus struct {
	Connected bool           `json:"connected"`
	Stats     map[string]any `json:"stats,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// NewHealthReady returns a handler that checks DB connectivity.
// We pass the DB in as a dependency — the handler doesn't create it.
func NewHealthReady(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.FromContext(r.Context())

		resp := ReadyResponse{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		// Check database connectivity.
		if err := database.Ping(r.Context()); err != nil {
			log.Error("readiness check failed — database unreachable",
				slog.String(logger.FieldError, err.Error()),
				slog.String(logger.FieldAction, "health_ready_fail"),
			)

			resp.Status = "not ready"
			resp.Database = DatabaseStatus{
				Connected: false,
				Error:     "database unreachable",
			}

			writeJSON(w, http.StatusServiceUnavailable, resp)
			return
		}

		resp.Status = "ready"
		resp.Database = DatabaseStatus{
			Connected: true,
			Stats:     database.Stats(),
		}

		log.Debug("readiness check passed",
			slog.String(logger.FieldAction, "health_ready_ok"),
		)

		writeJSON(w, http.StatusOK, resp)
	}
}
