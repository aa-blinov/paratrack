package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/httpjson"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
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
	list, err := s.services.Projects.Queries.List(r.Context(), tid, includeArchived)
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, projectListResponse{Projects: projectsFor(r, list)})
}

// handleAPIProjectCreate — POST /api/projects
// Body (form-encoded or JSON): name, slug (optional), color (optional).
func (s *Server) handleAPIProjectCreate(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	callerID := authenticatedUserID(r)
	var name, slug, color string
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			Name  string `json:"name"`
			Slug  string `json:"slug"`
			Color string `json:"color"`
		}
		if err := httpjson.Decode(r.Body, httpjson.MaxRequestBytes, &body); err != nil {
			s.writeProjectJSONDecodeError(w, err)
			return
		}
		name, slug, color = body.Name, body.Slug, body.Color
	} else {
		if err := r.ParseForm(); err != nil {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
			return
		}
		name, slug, color = r.Form.Get("name"), r.Form.Get("slug"), r.Form.Get("color")
	}
	p, err := s.services.Projects.Commands.Create(r.Context(), appmodel.ProjectCreateRequest{
		TeamID: tid, CallerID: callerID, Name: name, Slug: slug, Color: color,
	})
	if err != nil {
		if errors.Is(err, model.ErrAlreadyExists) {
			w.WriteHeader(http.StatusConflict)
			s.writeJSON(w, apiErrorResponse{Error: "a project with that slug or name already exists"})
			return
		}
		if errors.Is(err, model.ErrForbidden) {
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "manager role required"})
			return
		}
		if isProjectInputError(err) {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: err.Error()})
			return
		}
		s.writeInternalJSONError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	s.writeJSON(w, projectFor(r, p))
}

// handleAPIProjectUpdate — PATCH /api/projects/{id}
// Body: name, color, archived (any subset).
func (s *Server) handleAPIProjectUpdate(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	callerID := authenticatedUserID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.writeJSON(w, apiErrorResponse{Error: "invalid id"})
		return
	}
	update := appmodel.ProjectUpdate{}
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			Name     *string `json:"name"`
			Color    *string `json:"color"`
			Archived *bool   `json:"archived"`
		}
		if err := httpjson.Decode(r.Body, httpjson.MaxRequestBytes, &body); err != nil {
			s.writeProjectJSONDecodeError(w, err)
			return
		}
		if body.Name != nil {
			update.Name = *body.Name
		}
		if body.Color != nil {
			update.Color = *body.Color
		}
		update.Archived = body.Archived
	} else {
		if err := r.ParseForm(); err != nil {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
			return
		}
		update.Name = r.Form.Get("name")
		update.Color = r.Form.Get("color")
		if v := r.Form.Get("archived"); v != "" {
			b := v == "1" || v == "true"
			update.Archived = &b
		}
	}
	p, err := s.services.Projects.Commands.Update(r.Context(), appmodel.ProjectUpdateRequest{TeamID: tid, ProjectID: id, CallerID: callerID, Update: update})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			s.writeJSON(w, apiErrorResponse{Error: "project not found"})
			return
		}
		if errors.Is(err, model.ErrForbidden) {
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "manager role required"})
			return
		}
		if isProjectInputError(err) {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: err.Error()})
			return
		}
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, projectFor(r, p))
}

// handleAPIProjectDelete — DELETE /api/projects/{id}
func (s *Server) handleAPIProjectDelete(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	callerID := authenticatedUserID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.writeJSON(w, apiErrorResponse{Error: "invalid id"})
		return
	}
	if err := s.services.Projects.Commands.Delete(r.Context(), appmodel.ProjectMutationRequest{TeamID: tid, ProjectID: id, CallerID: callerID}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			s.writeJSON(w, apiErrorResponse{Error: "project not found"})
			return
		}
		if errors.Is(err, model.ErrForbidden) {
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "manager role required"})
			return
		}
		s.writeInternalJSONError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAPIAssignActivityProject — POST /api/activities/{id}/project
// Body: project_id (0 to clear).
func (s *Server) handleAPIAssignActivityProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.writeJSON(w, apiErrorResponse{Error: "invalid activity id"})
		return
	}
	var pid int64
	if ct := r.Header.Get("Content-Type"); ct == "application/json" {
		var body struct {
			ProjectID int64 `json:"project_id"`
		}
		if err := httpjson.Decode(r.Body, httpjson.MaxRequestBytes, &body); err != nil {
			s.writeProjectJSONDecodeError(w, err)
			return
		}
		pid = body.ProjectID
	} else {
		if err := r.ParseForm(); err != nil {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
			return
		}
		if v := r.Form.Get("project_id"); v != "" {
			pid, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.writeJSON(w, apiErrorResponse{Error: "invalid project_id"})
				return
			}
		}
	}
	if err := s.services.Projects.Commands.AssignActivity(r.Context(), appmodel.AssignActivityProjectRequest{
		TeamID: teamID(r), ActivityID: id, ProjectID: pid, CallerID: authenticatedUserID(r),
	}); err != nil {
		if errors.Is(err, model.ErrAlreadyBilled) {
			w.WriteHeader(http.StatusConflict)
			s.writeJSON(w, apiErrorResponse{Error: i18n.T(resolveLang(r), "inv.activityProjectLocked")})
			return
		}
		if errors.Is(err, model.ErrProjectRebindForbidden) {
			w.WriteHeader(http.StatusForbidden)
			s.writeJSON(w, apiErrorResponse{Error: i18n.T(resolveLang(r), "act.rebindForbidden")})
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			s.writeJSON(w, apiErrorResponse{Error: "project or activity not found"})
			return
		}
		s.writeInternalJSONError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeProjectJSONDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, httpjson.ErrPayloadTooLarge) {
		s.writeJSONStatus(w, http.StatusRequestEntityTooLarge, apiErrorResponse{Error: "request body too large"})
		return
	}
	s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid JSON"})
}

func isProjectInputError(err error) bool {
	return errors.Is(err, appmodel.ErrInvalidProjectName) ||
		errors.Is(err, appmodel.ErrInvalidProjectSlug) ||
		errors.Is(err, appmodel.ErrInvalidProjectColor) ||
		errors.Is(err, appmodel.ErrInvalidProjectRate) ||
		errors.Is(err, appmodel.ErrInvalidProjectCurrency) ||
		errors.Is(err, appmodel.ErrInvalidProjectEstimate)
}
