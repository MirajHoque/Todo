// Package models defines the core domain types for the todo application.
/*
 domain types" it just means "types that describe the real world problem"
 — Users and Todos, not HTTP or SQL.
*/

// models.go describes what things are (User, Todo) and what makes them valid.
// It does not care how they travel over the internet or how they are stored in a database.

// Rules for this package:
//   - No database logic here. Models describe the DOMAIN, not the DB schema.
//   - No HTTP logic here. Models are not JSON request/response types.
//   - Validation lives here because it belongs to the domain, not to a layer.
package models

import (
	"fmt"
	"strings"
	"time"
)

// ---- User -------------------------------------------------------------------

// User represents an authenticated account in the system.
// name,type, sturct tag
// Struct tags are metadata that tell the encoder/decoder how to name or handle fields.
//
//	tags only control the keys
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // never serialise the hash
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Validate checks that a User is internally consistent.
func (u *User) Validate() error {
	u.Email = strings.TrimSpace(strings.ToLower(u.Email))
	if u.Email == "" {
		return fmt.Errorf("user: email is required")
	}
	if !strings.Contains(u.Email, "@") {
		return fmt.Errorf("user: %q is not a valid email address", u.Email)
	}
	return nil
}

// ---- Todo -------------------------------------------------------------------

// Priority controls how urgent a todo item is.
// Custom Type, replicate Enum in C#/Java
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// Todo represents a single task owned by a user.
type Todo struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Priority    Priority   `json:"priority"`
	Done        bool       `json:"done"`
	DoneAt      *time.Time `json:"done_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Validate checks that a Todo is internally consistent.
func (t *Todo) Validate() error {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return fmt.Errorf("todo: title is required")
	}
	if len(t.Title) > 255 {
		return fmt.Errorf("todo: title must be 255 characters or fewer")
	}
	if t.Priority == "" {
		t.Priority = PriorityMedium
	}
	switch t.Priority {
	case PriorityLow, PriorityMedium, PriorityHigh:
		// valid
	default:
		return fmt.Errorf("todo: invalid priority %q, must be low, medium, or high", t.Priority)
	}
	return nil
}

// Complete marks a todo as done and records the timestamp.
func (t *Todo) Complete() {
	now := time.Now().UTC()
	t.Done = true
	t.DoneAt = &now
	t.UpdatedAt = now
}

// ---- Request / Response types -----------------------------------------------
// These are the HTTP layer types — separate from domain models on purpose.
// If the API contract changes, only these change, not the domain model.

// RegisterRequest is the body expected on POST /api/v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *RegisterRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if r.Email == "" {
		return fmt.Errorf("email is required")
	}
	if !strings.Contains(r.Email, "@") {
		return fmt.Errorf("%q is not a valid email", r.Email)
	}
	if len(r.Password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

// LoginRequest is the body expected on POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse is returned after a successful login or register.
type AuthResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

// CreateTodoRequest is the body expected on POST /api/v1/todos.
type CreateTodoRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    Priority `json:"priority"`
}

func (r *CreateTodoRequest) Validate() error {
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		return fmt.Errorf("title is required")
	}
	if len(r.Title) > 255 {
		return fmt.Errorf("title must be 255 characters or fewer")
	}
	return nil
}

// UpdateTodoRequest is the body expected on PATCH /api/v1/todos/:id.
type UpdateTodoRequest struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Priority    *Priority `json:"priority,omitempty"`
	Done        *bool     `json:"done,omitempty"`
}

// ErrorResponse is the standard error envelope returned on 4xx/5xx.
type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}
