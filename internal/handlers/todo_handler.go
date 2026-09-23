// todo_handler.go — HTTP handlers for todo CRUD operations.
//
// All routes here are protected — the JWT middleware already ran
// before any of these handlers execute. So we can safely call
// middleware.GetUserID(ctx) and trust the result.
//
// IMPORTANT SECURITY RULE
// ========================
// Every database call passes BOTH the todo ID and the user ID.
// The repository checks both — so a user can never read, update,
// or delete another user's todo, even if they guess the UUID.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/middleware"
	"github.com/MirajHoque/todo-app/internal/models"
	"github.com/go-chi/chi/v5"
)

// TodoHandler holds dependencies for todo endpoints.
type TodoHandler struct {
	todos *db.TodoRepository
}

// NewTodoHandler creates a TodoHandler.
func NewTodoHandler(todos *db.TodoRepository) *TodoHandler {
	return &TodoHandler{todos: todos}
}

// ── Create ────────────────────────────────────────────────────────────────────

// Create handles POST /api/v1/todos
//
// Request:  {"title": "Buy milk", "description": "", "priority": "high"}
// Response: the created todo with ID and timestamps
func (h *TodoHandler) Create(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())

	// Decode request body
	var req models.CreateTodoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if err := req.Validate(); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Insert into database
	todo, err := h.todos.Create(r.Context(), userID, &req)
	if err != nil {
		log.Error("failed to create todo",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "todo_create_fail"),
			slog.String(logger.FieldUserID, userID),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("todo created",
		slog.String(logger.FieldAction, "todo_created"),
		slog.String("todo_id", todo.ID),
		slog.String("priority", string(todo.Priority)),
	)

	writeJSON(w, http.StatusCreated, todo)
}

// ── List ──────────────────────────────────────────────────────────────────────

// List handles GET /api/v1/todos
//
// Returns all todos for the logged-in user, newest first.
// Response: array of todos — empty array [] if none exist
func (h *TodoHandler) List(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())

	todos, err := h.todos.ListByUser(r.Context(), userID)
	if err != nil {
		log.Error("failed to list todos",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "todo_list_fail"),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("todos listed",
		slog.String(logger.FieldAction, "todo_listed"),
		slog.Int("count", len(todos)),
	)

	writeJSON(w, http.StatusOK, todos)
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update handles PATCH /api/v1/todos/:id
//
// Partial update — only send the fields you want to change.
// Request:  {"done": true}  or  {"title": "New title", "priority": "low"}
// Response: the updated todo
func (h *TodoHandler) Update(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())

	// chi.URLParam reads the :id from the route path
	todoID := chi.URLParam(r, "id")
	if todoID == "" {
		writeError(w, r, http.StatusBadRequest, "todo id is required")
		return
	}

	// Decode partial update request
	var req models.UpdateTodoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	// Update in database — repository checks user ownership
	todo, err := h.todos.Update(r.Context(), todoID, userID, &req)
	if err != nil {
		if errors.Is(err, db.ErrTodoNotFound) {
			writeError(w, r, http.StatusNotFound, "todo not found")
			return
		}
		log.Error("failed to update todo",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "todo_update_fail"),
			slog.String("todo_id", todoID),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("todo updated",
		slog.String(logger.FieldAction, "todo_updated"),
		slog.String("todo_id", todo.ID),
		slog.Bool("done", todo.Done),
	)

	writeJSON(w, http.StatusOK, todo)
}

// ── Delete ────────────────────────────────────────────────────────────────────

// Delete handles DELETE /api/v1/todos/:id
//
// Permanently removes a todo.
// Returns 404 if the todo doesn't exist or belongs to another user.
func (h *TodoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	userID := middleware.GetUserID(r.Context())
	todoID := chi.URLParam(r, "id")

	if todoID == "" {
		writeError(w, r, http.StatusBadRequest, "todo id is required")
		return
	}

	if err := h.todos.Delete(r.Context(), todoID, userID); err != nil {
		if errors.Is(err, db.ErrTodoNotFound) {
			writeError(w, r, http.StatusNotFound, "todo not found")
			return
		}
		log.Error("failed to delete todo",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "todo_delete_fail"),
			slog.String("todo_id", todoID),
		)
		writeError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	log.Info("todo deleted",
		slog.String(logger.FieldAction, "todo_deleted"),
		slog.String("todo_id", todoID),
	)

	// 204 No Content — success but nothing to return
	w.WriteHeader(http.StatusNoContent)
}
