// Package api is the thin HTTP shell over the resolver (TECH_STACK.md).
// It owns: routing, request validation, the error envelope, and trace
// persistence — never resolution logic itself.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
)

// Deps carries constructor-injected dependencies (AGENTS.md: feature packages
// never import zerolog directly — they receive a logger from here).
type Deps struct {
	Store  *repo.Store
	Logger zerolog.Logger
}

// New builds the chi router with request-ID + structured access logging.
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealth)
	r.Get("/readyz", handleReady(d))

	r.Route("/employees", func(r chi.Router) {
		r.Post("/", handleCreateEmployee(d))
		r.Route("/{employeeID}", func(r chi.Router) {
			r.Post("/facts", handleAddFact(d))
			r.Get("/assignments", handleAssignments(d))
			r.Get("/explain", handleExplain(d))
		})
	})
	r.Route("/rules", func(r chi.Router) {
		r.Post("/", handleCreateRule(d))
		r.Post("/preview", handlePreview(d))
	})
	return r
}

// ---------------------------------------------------------------------------
// Error envelope — {"error":{"code":...,"message":...}} per docs/API.md
// ---------------------------------------------------------------------------

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// decodeBody strictly decodes a JSON body into dst.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// repoError maps well-known repo errors to HTTP statuses.
func repoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrNoTrace):
		writeError(w, http.StatusNotFound, "trace_not_found",
			"no decision trace stored for this (employee, category, date) — traces are written at decision time and never recomputed")
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Health endpoints
// ---------------------------------------------------------------------------

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReady(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Store.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
