package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------- Tags ---------------------------------------------------

// handleTagsList returns every tag as JSON.
func (s *Server) handleTagsList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.ListWithCounts(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	if tags == nil {
		tags = []appmodel.TagWithCount{}
	}
	responses := make([]tagResponse, 0, len(tags))
	for _, tag := range tags {
		responses = append(responses, tagResponse{ID: tag.ID, Name: tag.Name, TeamID: tag.TeamID, CreatedAt: tag.CreatedAt, SessionCount: tag.SessionCount})
	}
	s.writeJSON(w, tagListResponse{Tags: responses})
}

// handleTagsCreate adds a tag (or returns the existing one if the
// name is already taken). JSON body: {"name": "..."}.
// The page list is read from the JSON endpoint, so the response is JSON only
// refreshes in place; plain requests keep the JSON shape.
func (s *Server) handleTagsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	t, err := s.services.Tagging.Commands.CreateForMember(r.Context(), appmodel.TagCreateRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Name: name})
	if err != nil {
		s.writeTagCreateError(w, err)
		return
	}
	s.toastL(w, r, "toast.tagReady", t.Name, "success")
	s.writeJSON(w, tagFor(t))
}

// handleTagsDelete removes a tag by id. Query: ?id=N.
func (s *Server) handleTagsDelete(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimSpace(r.URL.Query().Get("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "id query param required (int64)", 400)
		return
	}
	if err := s.services.Tagging.Commands.Delete(r.Context(), appmodel.TagDeleteRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), TagID: id}); err != nil {
		if errors.Is(err, appmodel.ErrTagNotFound) {
			http.Error(w, "tag not found", 404)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.tagDeleted", "", "success")
	w.WriteHeader(200)
}

// handleTagsRename relabels a tag by id, keeping the sessions that carry it.
// Form: id=N&name=…  HTMX callers get the `tags-list` fragment back so the
// page list refreshes in place; plain requests keep the JSON shape.
func (s *Server) handleTagsRename(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	id, err := parseID(strings.TrimSpace(r.FormValue("id")))
	if err != nil {
		http.Error(w, "id query param required (int64)", 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	t, err := s.services.Tagging.Commands.Rename(r.Context(), appmodel.TagRenameRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), TagID: id, Name: name})
	if err != nil {
		s.writeTagRenameError(w, r, err)
		return
	}
	s.toastL(w, r, "toast.tagRenamed", t.Name, "success")
	s.writeJSON(w, tagFor(t))
}

// handleSessionTagAdd attaches a tag (auto-created if new) to a session.
// Body: name=...  The stats page reloads once it succeeds, so there is no body.
func (s *Server) handleSessionTagAdd(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	if err := s.services.Tagging.Commands.AttachForMember(r.Context(), appmodel.SessionTagRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), SessionID: id, Name: name}); err != nil {
		s.writeTagSessionError(w, err)
		return
	}
	s.toastL(w, r, "toast.tagged", name, "success")
	w.WriteHeader(http.StatusNoContent)
}

// handleSessionTagRemove detaches a tag from a session. Query: ?name=...
func (s *Server) handleSessionTagRemove(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "name query param required", 400)
		return
	}
	if err := s.services.Tagging.Commands.DetachForMember(r.Context(), appmodel.SessionTagRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), SessionID: id, Name: name}); err != nil {
		s.writeTagSessionError(w, err)
		return
	}
	s.toastL(w, r, "toast.untagged", name, "success")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeTagSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound), errors.Is(err, appmodel.ErrTagNotFound):
		http.Error(w, "session or tag not found", http.StatusNotFound)
	case errors.Is(err, model.ErrForbidden):
		http.Error(w, "workspace membership required", http.StatusForbidden)
	default:
		s.writeInternalError(w, err)
	}
}

func (s *Server) writeTagCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appmodel.ErrInvalidTagTeam), errors.Is(err, appmodel.ErrInvalidTag):
		http.Error(w, "invalid tag", http.StatusBadRequest)
	case errors.Is(err, model.ErrForbidden):
		http.Error(w, "workspace membership required", http.StatusForbidden)
	default:
		s.writeInternalError(w, err)
	}
}

// writeTagRenameError keeps the rename conflicts distinguishable: a taken name
// is the caller's to fix, so it gets a translated sentence instead of the
// internal error text the React page would otherwise show verbatim.
func (s *Server) writeTagRenameError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, appmodel.ErrTagNameTaken):
		http.Error(w, i18n.T(resolveLang(r), "err.tagNameTaken"), http.StatusConflict)
	case errors.Is(err, appmodel.ErrInvalidTagTeam), errors.Is(err, appmodel.ErrInvalidTag):
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
	case errors.Is(err, appmodel.ErrTagNotFound):
		http.Error(w, "tag not found", http.StatusNotFound)
	case errors.Is(err, model.ErrForbidden):
		http.Error(w, "workspace membership required", http.StatusForbidden)
	default:
		s.writeInternalError(w, err)
	}
}

// handleTagsPage serves /tags.
func (s *Server) handleTagsPage(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.ListWithCounts(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	views := make([]tagWithCount, 0, len(tags))
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		views = append(views, tagWithCount{
			tagChip:      tagChip{ID: t.ID, Name: t.Name},
			SessionCount: t.SessionCount,
			Lang:         string(resolveLang(r)),
		})
		names = append(names, t.Name)
	}
	data := &tagsData{
		pageData:    pageData{Title: "Tags", Active: "tags"},
		Tags:        views,
		AllTagNames: names,
		ReactTags:   true,
	}
	data.ReactApp = true
	s.renderPageForRequest(w, r, data.Title, data.Active, data)
}
