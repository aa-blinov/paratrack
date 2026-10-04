package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

type invalidSessionUpdateError string

const (
	invalidSessionUpdateBadID       invalidSessionUpdateError = "bad id"
	invalidSessionUpdateBadForm     invalidSessionUpdateError = "invalid form"
	invalidSessionUpdateBadStart    invalidSessionUpdateError = "bad start"
	invalidSessionUpdateBadEnd      invalidSessionUpdateError = "bad end"
	invalidSessionUpdateBadDuration invalidSessionUpdateError = "bad duration"
)

func (e invalidSessionUpdateError) Error() string { return string(e) }

func sessionUpdateClientError(err invalidSessionUpdateError) (message, translationKey string) {
	switch err {
	case invalidSessionUpdateBadID:
		return string(err), "err.invalidInput"
	case invalidSessionUpdateBadForm:
		return string(err), "err.invalidInput"
	case invalidSessionUpdateBadStart:
		return string(err), "err.sessionBadStart"
	case invalidSessionUpdateBadEnd:
		return string(err), "err.sessionBadEnd"
	case invalidSessionUpdateBadDuration:
		return string(err), "err.sessionBadDuration"
	default:
		return "invalid form", "err.invalidInput"
	}
}

// applySessionUpdateRequest parses and applies the shared session-edit form.
// Callers own their response format: the UI renders an HTMX row, while API v1
// returns JSON.
func (s *Server) applySessionUpdateRequest(r *http.Request) (int64, error) {
	id, err := parseID(r.PathValue("id"))
	if err != nil || id <= 0 {
		return 0, invalidSessionUpdateBadID
	}
	session, err := s.services.Tracking.Queries.Session(r.Context(), teamID(r), id)
	if err != nil {
		return 0, err
	}
	if err := r.ParseForm(); err != nil {
		return 0, invalidSessionUpdateBadForm
	}
	startStr := r.FormValue("start_at")
	endStr := r.FormValue("end_at")
	durationStr := r.FormValue("duration")
	update := appmodel.SessionUpdate{Note: r.FormValue("note"), UpdatedAt: userNow(r)}
	var startTime time.Time
	hasStart := false
	if startStr != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", startStr, userLoc(r))
		if err != nil {
			return 0, invalidSessionUpdateBadStart
		}
		startTime, hasStart = t, true
		update.StartAt = &t
	}
	// A duration wins over end_at (end = start + duration below); setting
	// both would assign end_at twice, which Postgres rejects.
	if endStr != "" && durationStr == "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", endStr, userLoc(r))
		if err != nil {
			return 0, invalidSessionUpdateBadEnd
		}
		update.EndAt = &t
	}
	// Duration takes priority: it overrides end_at by computing
	// end = start + duration. If no start is given, fetch current.
	if durationStr != "" {
		secs, err := timeparse.ParseDuration(durationStr)
		if err != nil {
			return 0, invalidSessionUpdateBadDuration
		}
		if !hasStart {
			startTime = session.StartAt
		}
		newEnd := startTime.Add(time.Duration(secs) * time.Second)
		update.EndAt = &newEnd
		// Hand-editing the interval defines the tracked total too —
		// keep accumulated_seconds in lock-step so every aggregate agrees.
		update.AccumulatedSeconds = &secs
	} else if endStr != "" {
		if !hasStart {
			startTime = session.StartAt
		}
		span := int(update.EndAt.Sub(startTime).Seconds())
		if span < 0 {
			span = 0
		}
		update.AccumulatedSeconds = &span
	}
	if err := s.services.Tracking.Commands.UpdateFields(r.Context(), appmodel.SessionUpdateRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), SessionID: id, Update: update}); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Server) writeSessionUpdateError(w http.ResponseWriter, r *http.Request, err error) {
	if s.writeSessionLockConflict(w, r, err) {
		return
	}
	jsonResponse := strings.HasPrefix(r.URL.Path, "/api/v1/")
	writeClientError := func(status int, message, translationKey string) {
		if jsonResponse {
			s.writeJSONStatus(w, status, apiErrorResponse{Error: message})
			return
		}
		http.Error(w, i18n.T(resolveLang(r), translationKey), status)
	}
	var invalid invalidSessionUpdateError
	switch {
	case errors.As(err, &invalid):
		message, key := sessionUpdateClientError(invalid)
		writeClientError(http.StatusBadRequest, message, key)
	case errors.Is(err, appmodel.ErrInvalidSessionPeriod):
		writeClientError(http.StatusBadRequest, err.Error(), "err.endBeforeStart")
	case errors.Is(err, appmodel.ErrInvalidSessionLength):
		writeClientError(http.StatusBadRequest, err.Error(), "err.sessionBadDuration")
	case errors.Is(err, model.ErrNotFound):
		writeClientError(http.StatusNotFound, "session not found", "err.sessionNotFound")
	default:
		if jsonResponse {
			s.writeInternalJSONError(w, err)
		} else {
			s.writeInternalErrorMessage(w, err, i18n.T(resolveLang(r), "err.internal"))
		}
	}
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id, err := s.applySessionUpdateRequest(r)
	if err != nil {
		s.writeSessionUpdateError(w, r, err)
		return
	}
	// Re-render the single updated row (with tags + project badge).
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSessionRow(w, r, id)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.services.TrackingOps.Delete(operationContext(r), appmodel.SessionDeleteRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), SessionID: id}); err != nil {
		if s.writeSessionLockConflict(w, r, err) {
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "session not found", 404)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.deleted", "", "success")
	w.Header().Set("HX-Trigger", "sessions-changed")
	w.WriteHeader(200)
}
