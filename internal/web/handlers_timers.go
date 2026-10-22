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
	vm, err := s.buildActiveListVM(r)
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, vm)
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
	// The dashboard form asks once before a timer that would have no project (TIME-12).
	// The confirmed start goes through; API clients are not asked.
	if r.FormValue("ask_no_project") == "1" && projectIDFromForm(r) == 0 && r.FormValue("confirm_no_project") != "1" && !s.activityHasProject(r, name) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Start-Confirm", "no-project")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, i18n.T(resolveLang(r), "dash.startNoProjectConfirm"))
		return
	}
	act, err := s.startTimer(r, name, note)
	if err != nil {
		s.writeTimerStartError(w, r, act.Name, err)
		return
	}
	s.toastL(w, r, "toast.started", act.Name, "success")
	s.respondActiveList(w, r)
}

func (s *Server) startTimer(r *http.Request, name, note string) (model.Activity, error) {
	act, _, err := s.services.TrackingOps.StartActivity(operationContext(r), appmodel.TimerStartByNameRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: name,
		ProjectID: projectIDFromForm(r), At: actionTime(r), Note: note,
	})
	if err != nil {
		return act, err
	}
	s.markTaskStarted(r, act.ID)
	return act, nil
}

// markTaskStarted moves the task an activity belongs to into "in progress" when
// work starts on it (TIME-04). The timer is already running, so a failure here is
// logged, not reported to the person who started it.
func (s *Server) markTaskStarted(r *http.Request, activityID int64) {
	taskID, err := s.services.Tasks.TaskOfActivity(r.Context(), teamID(r), activityID)
	if err != nil || taskID <= 0 {
		return
	}
	if err := s.services.Tasks.StartWork(r.Context(), appmodel.TaskStartRequest{TeamID: teamID(r), TaskID: taskID, CallerID: authenticatedUserID(r)}); err != nil {
		s.logInternalError(fmt.Errorf("mark task %d started: %w", taskID, err))
	}
}

// activityHasProject reports whether time started on the named activity lands on
// a project: an existing activity's project, not the form's.
func (s *Server) activityHasProject(r *http.Request, name string) bool {
	act, err := s.services.Tracking.Queries.FindActivity(r.Context(), appmodel.ActivityNameQuery{TeamID: teamID(r), Name: name})
	return err == nil && act.ProjectID > 0
}

func projectIDFromForm(r *http.Request) int64 {
	if projectID, err := strconv.ParseInt(r.FormValue("project_id"), 10, 64); err == nil && projectID > 0 {
		return projectID
	}
	return 0
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
	if s.writeTaskArchived(w, r, err) {
		return
	}
	if errors.Is(err, model.ErrActiveSessionExists) {
		http.Error(w, fmt.Sprintf("%q already has an active session", activityName), http.StatusConflict)
		return
	}
	s.writeInternalError(w, err)
}

// writeTaskArchived answers a timer write aimed at an archived task's activity:
// the task is read-only until it is put back on the board. It reports whether
// err was that case.
func (s *Server) writeTaskArchived(w http.ResponseWriter, r *http.Request, err error) bool {
	if !errors.Is(err, appmodel.ErrTaskArchived) {
		return false
	}
	message := i18n.T(resolveLang(r), "tasks.archivedTracking")
	s.toast(w, message, "error")
	http.Error(w, message, http.StatusConflict)
	return true
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
	message := strings.NewReplacer("{name}", stopped.ActivityName, "{dur}", dur).
		Replace(i18n.T(resolveLang(r), "toast.stoppedFull"))
	// Time on an activity with no task is kept, but it is on no task: say so (TIME-05).
	if linked, err := s.services.Tasks.ActivityHasTask(ctx, teamID(r), stopped.Session.ActivityID); err == nil && !linked {
		message += " " + i18n.T(resolveLang(r), "toast.noTaskWarning")
	}
	s.toast(w, message, "success")
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
		if s.writeTaskArchived(w, r, err) {
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
	var approvalErr *model.SessionApprovalLockError
	if errors.As(err, &approvalErr) {
		msg := weekLockedMessage(resolveLang(r), userLoc(r), approvalErr)
		s.toast(w, msg, "error")
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: msg})
		} else {
			http.Error(w, msg, http.StatusConflict)
		}
		return true
	}
	if errors.Is(err, model.ErrAlreadyBilled) {
		msg := i18n.T(resolveLang(r), "ts.projectLockedByInvoice")
		s.toast(w, msg, "error")
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: msg})
		} else {
			http.Error(w, msg, http.StatusConflict)
		}
		return true
	}
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
	// Stopping every timer at once is the most mis-tappable action there is,
	// so it owes the same way back as a single stop.
	for _, id := range ids {
		w.Header().Add("X-Toast-Undo", "/api/sessions/"+strconv.FormatInt(id, 10)+"/reopen")
	}
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
		if s.writeTaskArchived(w, r, err) {
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.focused", act.Name, "success")
	s.respondActiveList(w, r)
}
