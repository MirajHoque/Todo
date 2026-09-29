// agent_handler.go — HTTP handler for the chat endpoint.
//
// Single endpoint: POST /api/v1/agent/chat
// Protected by JWT — user must be logged in.
//
// Request:  {"message": "add buy milk with high priority"}
// Response: {"reply": "Done! I've added 'Buy milk' to your todos."}
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/MirajHoque/todo-app/internal/agents"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
)

// AgentHandler handles chat messages routed through the orchestrator.
type AgentHandler struct {
	orchestrator *agents.Orchestrator
}

// NewAgentHandler creates an AgentHandler.
func NewAgentHandler(orchestrator *agents.Orchestrator) *AgentHandler {
	return &AgentHandler{orchestrator: orchestrator}
}

// ChatRequest is what the browser sends.
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatResponse is what we send back.
type ChatResponse struct {
	Reply string `json:"reply"`
}

// Chat handles POST /api/v1/agent/chat
func (h *AgentHandler) Chat(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())

	// Decode request
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		writeError(w, r, http.StatusBadRequest, "message is required")
		return
	}

	log.Info("agent chat request",
		slog.String(logger.FieldAction, "agent_chat"),
		slog.String(logger.FieldUserID, userID),
	)

	// Route through orchestrator
	reply, err := h.orchestrator.Handle(r.Context(), userID, req.Message)
	if err != nil {
		log.Error("agent chat failed",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "agent_chat_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "agent failed to respond")
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{Reply: reply})
}
