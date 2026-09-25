// page_handler.go — serves the HTML template pages.
//
// HOW GO TEMPLATES WORK
// =====================
// html/template reads .html files and renders them with data you pass in.
// The base.html file defines the overall page layout with named blocks:
//
//	{{block "title" .}}  — page title
//	{{block "nav" .}}    — navigation links
//	{{block "content" .}} — main content
//	{{block "scripts" .}} — JavaScript
//
// Each page template (login.html, todos.html) fills in those blocks.
// The dot (.) in templates is the data you pass from Go — currently empty
// but we can add server-side data later (e.g. flash messages).
package handlers

import (
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"

	"github.com/MirajHoque/todo-app/internal/logger"
)

// PageHandler serves HTML pages using Go templates.
type PageHandler struct {
	templatesDir string
}

// NewPageHandler creates a PageHandler.
// It finds the templates directory relative to the project root.
func NewPageHandler() *PageHandler {
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	return &PageHandler{
		templatesDir: filepath.Join(projectRoot, "web", "templates"),
	}
}

// render parses and executes a template, writing the result to w.
// Always parses base.html first — every page extends it.
func (h *PageHandler) render(w http.ResponseWriter, r *http.Request, page string, data any) {
	log := logger.FromContext(r.Context())

	// Parse base layout + the specific page template together.
	// They must be parsed together so the block definitions work.
	tmpl, err := template.ParseFiles(
		filepath.Join(h.templatesDir, "base.html"),
		filepath.Join(h.templatesDir, page),
	)
	if err != nil {
		log.Error("failed to parse template",
			slog.String(logger.FieldError, err.Error()),
			slog.String("template", page),
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Execute renders the template with the data and writes to w.
	// We execute "base.html" (not the page name) because base.html
	// is the outer wrapper that pulls in the page blocks.
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		log.Error("failed to execute template",
			slog.String(logger.FieldError, err.Error()),
			slog.String("template", page),
		)
	}
}

// ── Page handlers ─────────────────────────────────────────────────────────────

// LoginPage serves GET /
func (h *PageHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "login.html", nil)
}

// RegisterPage serves GET /register
func (h *PageHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "register.html", nil)
}

// TodosPage serves GET /todos
func (h *PageHandler) TodosPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "todos.html", nil)
}
