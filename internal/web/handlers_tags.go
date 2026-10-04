package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------- Tags ---------------------------------------------------

// handleTagsList returns every tag as JSON.
func (s *Server) handleTagsList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.List(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	if tags == nil {
		tags = []model.Tag{}
	}
	s.writeJSON(w, tagListResponse{Tags: tagsFor(tags)})
}

// handleTagsCreate adds a tag (or returns the existing one if the
// name is already taken). JSON body: {"name": "..."}.
// HTMX callers get the `tags-list` fragment back so the page list
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
	if isHTMX(r) {
		s.respondTagsList(w, r)
		return
	}
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
	if isHTMX(r) {
		s.respondTagsList(w, r)
		return
	}
	w.WriteHeader(200)
}

// respondTagsList renders the `tags-list` fragment for HTMX swaps.
func (s *Server) respondTagsList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.ListWithCounts(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	views := make([]tagWithCount, 0, len(tags))
	for _, t := range tags {
		views = append(views, tagWithCount{
			tagChip:      tagChip{ID: t.ID, Name: t.Name},
			SessionCount: t.SessionCount,
			Lang:         string(resolveLang(r)),
		})
	}
	s.renderFragment(w, "tags-list", tagsListVM{Lang: string(resolveLang(r)), Tags: views, CanManage: canManage(r)})
}

// handleSessionTagAdd attaches a tag (auto-created if new) to a session.
// Body: name=...  HTMX swaps the response into the session row.
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
	s.respondSessionRow(w, r, id)
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
	s.respondSessionRow(w, r, id)
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

// respondSessionRow re-renders one stats table row (tags + project
// badge included) so HTMX outerHTML swaps keep the row intact.
func (s *Server) respondSessionRow(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	snapshot, err := s.services.SessionDecorations.BuildRow(ctx, appmodel.SessionLookupQuery{TeamID: teamID(r), SessionID: id})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	now := userNow(r)
	period := s.parsePeriodAt(r, now)
	views := []sessionView{toSessionView(snapshot.Session.Session, snapshot.Session.Activity, period.Start, period.End, now, resolveLang(r), durFmtOf(r))}
	attachSessionTags(views, snapshot.Decorations.TagsBySession)
	attachSessionProjects(views, snapshot.Decorations.ProjectsByID)
	views[0].Lang = string(resolveLang(r))
	fragment, err := s.executeTemplate("session-row", views[0])
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(fragment); err != nil {
		s.logger.Printf("web: write session row: %v", err)
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
	s.render(w, r, "tags-content", &tagsData{
		pageData:    pageData{Title: "Tags", Active: "tags"},
		Tags:        views,
		AllTagNames: names,
	})
}

// handleTagsFragment returns the inner `tags-list` template so HTMX
// can swap it without a full page reload.
func (s *Server) handleTagsFragment(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.ListWithCounts(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	views := make([]tagWithCount, 0, len(tags))
	for _, t := range tags {
		views = append(views, tagWithCount{
			tagChip:      tagChip{ID: t.ID, Name: t.Name},
			SessionCount: t.SessionCount,
			Lang:         string(resolveLang(r)),
		})
	}
	s.renderFragment(w, "tags-list", tagsListVM{Lang: string(resolveLang(r)), Tags: views, CanManage: canManage(r)})
}
