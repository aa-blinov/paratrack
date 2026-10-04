package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Saved reports
// ---------------------------------------------------------------------------

// handleSavedReportsCreate stores the current /stats filters as a preset.
func (s *Server) handleSavedReportsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/stats?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	period := strings.TrimSpace(r.PostForm.Get("period"))
	project := strings.TrimSpace(r.PostForm.Get("project"))
	tag := strings.TrimSpace(r.PostForm.Get("tag"))
	uid := int64(0)
	if u, ok := UserFrom(r.Context()); ok {
		uid = u.ID
	}
	if name == "" {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, "name is required"), http.StatusSeeOther)
		return
	}
	if _, err := s.services.Reports.Create(r.Context(), appmodel.SavedReportCreateRequest{
		TeamID: teamID(r), ActorID: uid, Name: name, Period: period, ProjectSlug: project, Tag: tag,
	}); err != nil {
		msg := i18n.T(resolveLang(r), "err.internal")
		if errors.Is(err, appmodel.ErrInvalidSavedReport) {
			msg = i18n.T(resolveLang(r), "err.invalidInput")
		} else {
			s.logInternalError(err)
		}
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	// Back to the same stats view.
	next := "/stats?period=" + url.QueryEscape(period)
	if project != "" {
		next += "&project=" + url.QueryEscape(project)
	}
	if tag != "" {
		next += "&tag=" + url.QueryEscape(tag)
	}
	http.Redirect(w, r, next+"&flash="+encodeFlash(true, "report saved"), http.StatusSeeOther)
}

// handleSavedReportsDelete removes a preset.
func (s *Server) handleSavedReportsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	me, _ := UserFrom(r.Context())
	if err := s.services.Reports.Delete(r.Context(), appmodel.SavedReportDeleteRequest{TeamID: teamID(r), ReportID: id, CallerID: me.ID}); err != nil {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, "not found"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/stats?flash="+encodeFlash(true, "report deleted"), http.StatusSeeOther)
}

// loadSavedReports is a helper used by handleStats.
func (s *Server) loadSavedReports(r *http.Request) ([]model.SavedReport, error) {
	return s.services.Reports.List(r.Context(), teamID(r))
}
