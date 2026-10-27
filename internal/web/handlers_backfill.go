package web

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// longSessionLimit is the length past which a session is worth splitting.
const longSessionLimit = 10 * time.Hour

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
	// A session longer than ten hours is usually a timer left running (TIME-18).
	// The form asks once and sends confirm_long=1 with the second save; the API
	// does not ask unless a client sends the field without it.
	if end.Sub(start) > longSessionLimit && r.FormValue("confirm_long") != "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Backfill-Confirm", "long")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, i18n.T(resolveLang(r), "stats.longSessionConfirm"))
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
		if errors.Is(err, appmodel.ErrTaskArchived) {
			s.backfillError(w, r, "activity", i18n.T(resolveLang(r), "tasks.archivedTracking"))
			return
		}
		if errors.Is(err, appmodel.ErrInvalidSessionStart) {
			s.backfillError(w, r, "activity", i18n.T(resolveLang(r), "err.backfillRequired"))
			return
		}
		if errors.Is(err, appmodel.ErrSessionEndInFuture) {
			s.backfillError(w, r, "end", i18n.T(resolveLang(r), "err.sessionEndFuture"))
			return
		}
		if errors.Is(err, appmodel.ErrInvalidSessionPeriod) {
			s.backfillError(w, r, "end", i18n.T(resolveLang(r), "err.sessionEndBeforeStart"))
			return
		}
		if errors.Is(err, model.ErrProjectRebindForbidden) {
			s.backfillError(w, r, "form", i18n.T(resolveLang(r), "act.rebindForbidden"))
			return
		}
		var approvalErr *model.SessionApprovalLockError
		if errors.As(err, &approvalErr) {
			s.backfillError(w, r, "form", weekLockedMessage(resolveLang(r), userLoc(r), approvalErr))
			return
		}
		var autoLockErr *model.SessionAutoLockError
		if errors.As(err, &autoLockErr) {
			s.backfillError(w, r, "form", autoLockMessage(resolveLang(r), autoLockErr))
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
	detail := act.Name + " " + start.Format("15:04") + "→" + end.Format("15:04")
	if linked, err := s.services.Tasks.ActivityHasTask(r.Context(), teamID(r), act.ID); err == nil && !linked {
		detail += ". " + i18n.T(resolveLang(r), "toast.noTaskWarning")
	}
	s.toastL(w, r, "toast.added", detail, "success")
	// Refresh the active list (unchanged) so HTMX has a target; the
	// user then looks at /stats for the closed row.
	s.respondActiveList(w, r)
}

// handleSetLang switches the UI language and returns to the previous page.
