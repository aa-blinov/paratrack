package web

import (
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

func (s *Server) handleAPIActive(w http.ResponseWriter, r *http.Request) {
	s.respondActiveList(w, r)
}

// actionTime is when a timer action happened: the client's click time
// for actions replayed from the offline queue ("client_ts", unix ms),
// otherwise now. Only the last 24h is trusted.
func actionTime(r *http.Request) time.Time {
	now := userNow(r)
	ms, err := strconv.ParseInt(r.FormValue("client_ts"), 10, 64)
	if err != nil {
		return now
	}
	t := time.UnixMilli(ms)
	if t.After(now) || now.Sub(t) > 24*time.Hour {
		return now
	}
	return t
}

// ---------- POST /api/start ---------------------------------------

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("activity"))
	if name == "" {
		http.Error(w, "activity is required", 400)
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	act, err := s.startTimer(r, name, note)
	if err != nil {
		s.writeTimerStartError(w, r, act.Name, err)
		return
	}
	s.toastL(w, r, "toast.started", act.Name, "success")
	s.respondActiveList(w, r)
}

func (s *Server) startTimer(r *http.Request, name, note string) (model.Activity, error) {
	var projectID int64
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		if parsed, err := strconv.ParseInt(pidStr, 10, 64); err == nil && parsed > 0 {
			projectID = parsed
		}
	}
	act, _, err := s.services.TrackingOps.StartActivity(operationContext(r), appmodel.TimerStartByNameRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: name,
		ProjectID: projectID, At: actionTime(r), Note: note,
	})
	if err != nil {
		return act, err
	}
	return act, nil
}

func (s *Server) writeTimerStartError(w http.ResponseWriter, r *http.Request, activityName string, err error) {
	// Surface explicit project selections and duplicate timers as client errors.
	if errors.Is(err, model.ErrProjectRebindForbidden) {
		message := i18n.T(resolveLang(r), "act.rebindForbidden")
		s.toast(w, message, "error")
		http.Error(w, message, http.StatusForbidden)
		return
	}
	if errors.Is(err, model.ErrAlreadyBilled) {
		message := i18n.T(resolveLang(r), "inv.activityProjectLocked")
		s.toast(w, message, "error")
		http.Error(w, message, http.StatusConflict)
		return
	}
	if errors.Is(err, model.ErrActiveSessionExists) {
		http.Error(w, fmt.Sprintf("%q already has an active session", activityName), http.StatusConflict)
		return
	}
	s.writeInternalError(w, err)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := operationContext(r)
	stopped, err := s.services.TrackingOps.Stop(ctx, appmodel.TimerStopRequest{TeamID: teamID(r), SessionID: id, At: actionTime(r)})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, model.ErrSessionNotActive) {
			http.Error(w, "already stopped", http.StatusConflict)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	// Say what stopped, how long it ran, and where it went.
	// A session that ran at all reads "<1 min", never "0 min".
	dur := fmtDur(r, max(1, stopped.DurationSeconds))
	s.toast(w, strings.NewReplacer("{name}", stopped.ActivityName, "{dur}", dur).
		Replace(i18n.T(resolveLang(r), "toast.stoppedFull")), "success")
	// Stop is instant; the toast carries the way back (and a discard for
	// accidental sub-minute sessions).
	w.Header().Set("X-Toast-Undo", "/api/sessions/"+strconv.FormatInt(id, 10)+"/reopen")
	if stopped.DurationSeconds < 60 {
		w.Header().Set("X-Toast-Discard", "/api/sessions/"+strconv.FormatInt(id, 10))
	}
	s.respondActiveList(w, r)
}

// handleReopen is the undo for handleStop.
func (s *Server) handleReopen(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := operationContext(r)
	if _, err := s.services.TrackingOps.Reopen(ctx, appmodel.TimerReopenRequest{TeamID: teamID(r), SessionID: id, At: userNow(r).UTC()}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, model.ErrSessionReopenExpired) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, model.ErrActiveSessionExists) {
			http.Error(w, "activity already has an active session", http.StatusConflict)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.reopened", "", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	if _, err := s.services.Tracking.Commands.Pause(ctx, appmodel.TimerSessionRequest{TeamID: teamID(r), SessionID: id, At: actionTime(r)}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, model.ErrSessionNotActive) {
			http.Error(w, "session already stopped", http.StatusConflict)
			return
		}
		if errors.Is(err, model.ErrSessionAlreadyPaused) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.paused", "", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	if _, err := s.services.Tracking.Commands.Resume(ctx, appmodel.TimerSessionRequest{TeamID: teamID(r), SessionID: id, At: actionTime(r)}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, model.ErrSessionNotActive) {
			http.Error(w, "session already stopped", http.StatusConflict)
			return
		}
		if errors.Is(err, model.ErrSessionNotPaused) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.resumed", "", "success")
	s.respondActiveList(w, r)
}

// writeSessionLockConflict maps the storage-enforced invoice lock to a
// transport response. Sent and paid invoice snapshots cannot be edited.
func (s *Server) writeSessionLockConflict(w http.ResponseWriter, r *http.Request, err error) bool {
	var lockErr *model.SessionInvoiceLockError
	if !errors.As(err, &lockErr) {
		return false
	}
	msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.locked"), lockErr.InvoiceNumber)
	s.toast(w, msg, "error")
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: msg})
	} else {
		http.Error(w, msg, http.StatusConflict)
	}
	return true
}

// handlePauseAll pauses every running timer (parallel timers need a way
// to stop the world, e.g. for a break).
func (s *Server) handlePauseAll(w http.ResponseWriter, r *http.Request) {
	ids, err := s.services.Tracking.Commands.PauseAll(r.Context(), appmodel.TimerStopAllRequest{TeamID: teamID(r), At: actionTime(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.toast(w, fmt.Sprintf(i18n.T(resolveLang(r), "toast.pausedAll"), len(ids)), "success")
	s.respondActiveList(w, r)
}

// handleStopAll stops every open timer at the same moment.
func (s *Server) handleStopAll(w http.ResponseWriter, r *http.Request) {
	ctx := operationContext(r)
	ids, err := s.services.TrackingOps.StopAll(ctx, appmodel.TimerStopAllRequest{TeamID: teamID(r), At: actionTime(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.toast(w, fmt.Sprintf(i18n.T(resolveLang(r), "toast.stoppedAll"), len(ids)), "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	act, _, err := s.services.TrackingOps.FocusActivity(operationContext(r), appmodel.TimerFocusByNameRequest{
		TeamID: teamID(r), ActivityName: name, At: actionTime(r),
	})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.focused", act.Name, "success")
	s.respondActiveList(w, r)
}
