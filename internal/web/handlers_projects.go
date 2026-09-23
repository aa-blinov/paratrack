package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/db"
)

// --- /api/projects JSON CRUD -----------------------------------------
//
// All endpoints are auth-required (RequireAuth middleware on the api
// subrouter) and team-scoped via teamID(r). The current UI uses
// HTMX + form posts for project edits, but the JSON surface lets
// external tools (CLI, future integrations) drive projects the same
// way the team-management endpoints already do.

// handleAPIProjectsList — GET /api/projects
// Optional query: ?archived=1 to include archived projects.
func (s *Server) handleAPIProjectsList(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	includeArchived := r.URL.Query().Get("archived") == "1"
	list, err := s.db.ListProjects(r.Context(), tid, includeArchived)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"projects": list})
}

// handleAPIProjectCreate — POST /api/projects
// Body (form-encoded or JSON): name, slug (optional), color (optional).
func (s *Server) handleAPIProjectCreate(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	var name, slug, color string
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			Name  string `json:"name"`
			Slug  string `json:"slug"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		name, slug, color = body.Name, body.Slug, body.Color
	} else {
		_ = r.ParseForm()
		name, slug, color = r.Form.Get("name"), r.Form.Get("slug"), r.Form.Get("color")
	}
	p, err := s.db.CreateProject(r.Context(), tid, name, slug, color)
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]string{"error": "a project with that slug or name already exists"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, p)
}

// handleAPIProjectUpdate — PATCH /api/projects/{id}
// Body: name, color, archived (any subset).
func (s *Server) handleAPIProjectUpdate(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid id"})
		return
	}
	var name, color string
	var archived *bool
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			Name     *string `json:"name"`
			Color    *string `json:"color"`
			Archived *bool   `json:"archived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if body.Name != nil {
			name = *body.Name
		}
		if body.Color != nil {
			color = *body.Color
		}
		archived = body.Archived
	} else {
		_ = r.ParseForm()
		name = r.Form.Get("name")
		color = r.Form.Get("color")
		if v := r.Form.Get("archived"); v != "" {
			b := v == "1" || v == "true"
			archived = &b
		}
	}
	p, err := s.db.UpdateProject(r.Context(), tid, id, name, color, archived)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "project not found"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, p)
}

// handleAPIProjectDelete — DELETE /api/projects/{id}
func (s *Server) handleAPIProjectDelete(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid id"})
		return
	}
	if err := s.db.DeleteProject(r.Context(), tid, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "project not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAPIAssignActivityProject — POST /api/activities/{id}/project
// Body: project_id (0 to clear).
func (s *Server) handleAPIAssignActivityProject(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid activity id"})
		return
	}
	var pid int64
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			ProjectID int64 `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		pid = body.ProjectID
	} else {
		_ = r.ParseForm()
		if v := r.Form.Get("project_id"); v != "" {
			pid, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid project_id"})
				return
			}
		}
	}
	if err := s.db.AssignActivityProject(r.Context(), tid, id, pid); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "project or activity not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
