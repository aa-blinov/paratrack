package web

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func (s *Server) backfillError(w http.ResponseWriter, r *http.Request, field, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Backfill-Field", field)
	fmt.Fprintf(w, `<span id="b-%s-error" hx-swap-oob="innerHTML">%s</span>`, field, html.EscapeString(message))
	s.respondActiveList(w, r)
}

// handleBackfill creates a finished session from the dashboard form.
// Accepts natural-language times (same parser as the CLI `add`).

func (s *Server) handleBackfill(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("activity"))
	startStr := strings.TrimSpace(r.FormValue("start"))
	endStr := strings.TrimSpace(r.FormValue("end"))
	note := strings.TrimSpace(r.FormValue("note"))
	if name == "" || startStr == "" || endStr == "" {
		field := "activity"
		if name != "" {
			field = "start"
			if startStr != "" {
				field = "end"
			}
		}
		s.backfillError(w, r, field, i18n.T(resolveLang(r), "err.backfillRequired"))
		return
	}
	now := userNow(r)
	start, err := timeparse.ParseDateTime(startStr, now)
	if err != nil {
		s.backfillError(w, r, "start", i18n.T(resolveLang(r), "err.badStart")+" "+startStr)
		return
	}
	end, err := timeparse.ParseDateTime(endStr, now)
	if err != nil {
		s.backfillError(w, r, "end", i18n.T(resolveLang(r), "err.badEnd")+" "+endStr)
		return
	}
	if !end.After(start) {
		s.backfillError(w, r, "end", i18n.T(resolveLang(r), "err.endBeforeStart"))
		return
	}
	var projectID int64
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		if parsed, err := strconv.ParseInt(pidStr, 10, 64); err == nil && parsed > 0 {
			projectID = parsed
		}
	}
	act, _, err := s.services.TrackingOps.AddClosedActivity(operationContext(r), appmodel.TimerAddByNameRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: name,
		ProjectID: projectID, Start: start, End: end, Note: note,
	})
	if err != nil {
		if errors.Is(err, appmodel.ErrInvalidSessionStart) {
			s.backfillError(w, r, "activity", i18n.T(resolveLang(r), "err.backfillRequired"))
			return
		}
		if errors.Is(err, appmodel.ErrInvalidSessionPeriod) || errors.Is(err, appmodel.ErrInvalidSessionStart) {
			s.backfillError(w, r, "form", err.Error())
			return
		}
		if errors.Is(err, model.ErrProjectRebindForbidden) {
			s.backfillError(w, r, "form", i18n.T(resolveLang(r), "act.rebindForbidden"))
			return
		}
		if errors.Is(err, model.ErrAlreadyBilled) {
			s.backfillError(w, r, "form", i18n.T(resolveLang(r), "inv.activityProjectLocked"))
			return
		}
		s.logInternalError(err)
		s.backfillError(w, r, "form", i18n.T(resolveLang(r), "err.internal"))
		return
	}
	w.Header().Set("X-Backfill-Saved", "true")
	s.toastL(w, r, "toast.added", act.Name+" "+start.Format("15:04")+"→"+end.Format("15:04"), "success")
	// Refresh the active list (unchanged) so HTMX has a target; the
	// user then looks at /stats for the closed row.
	s.respondActiveList(w, r)
}

// handleSetLang switches the UI language and returns to the previous page.
