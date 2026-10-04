package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// API v1 provides the stable JSON surface for scripts and the extension.
// handleAPIv1Sessions — GET list / POST create.
func (s *Server) handleAPIv1Sessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		from, to := rangeFromQuery(r)
		// Pages of ?limit= (default 100, max 500), newest first; pass the
		// returned next_cursor as ?cursor= for the next page.
		limit := 100
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
			limit = min(v, 500)
		}
		var after *model.SessionCursor
		if c := r.URL.Query().Get("cursor"); c != "" {
			var ok bool
			if after, ok = decodeSessionCursor(c); !ok {
				s.writeJSONStatus(w, 400, apiErrorResponse{Error: "bad cursor"})
				return
			}
		}
		page, err := s.services.Tracking.Queries.SessionHistoryPage(r.Context(), teamID(r), from, to, after, limit)
		if err != nil {
			s.writeInternalJSONError(w, err)
			return
		}
		out := []apiV1SessionResponse{}
		now := userNow(r)
		for _, as := range page.Items {
			note := ""
			if as.Session.Note != nil {
				note = *as.Session.Note
			}
			end := ""
			if as.Session.EndAt != nil {
				end = as.Session.EndAt.UTC().Format(time.RFC3339)
			}
			out = append(out, apiV1SessionResponse{
				ID: as.Session.ID, Activity: as.Activity.Name,
				Start: as.Session.StartAt.UTC().Format(time.RFC3339), End: end,
				Seconds: as.Session.DurationSeconds(now), Note: note,
			})
		}
		resp := apiV1SessionsResponse{Sessions: out}
		if page.NextCursor != nil {
			cursor := encodeSessionCursor(*page.NextCursor)
			resp.NextCursor = &cursor
		}
		s.writeJSON(w, resp)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
			return
		}
		name := strings.TrimSpace(r.FormValue("activity"))
		if name == "" {
			s.writeJSONStatus(w, 400, apiErrorResponse{Error: "activity is required"})
			return
		}
		ctx := operationContext(r)
		_, sess, err := s.services.TrackingOps.StartActivity(ctx, appmodel.TimerStartByNameRequest{
			TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: name,
			At: userNow(r), Note: r.FormValue("note"),
		})
		if err != nil {
			switch {
			case errors.Is(err, model.ErrActiveSessionExists):
				s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: "activity already has an active session"})
			case errors.Is(err, appmodel.ErrInvalidSessionStart):
				s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid timer start"})
			case errors.Is(err, model.ErrNotFound):
				s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "activity not found"})
			case errors.Is(err, model.ErrForbidden):
				s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "workspace membership required"})
			default:
				s.writeInternalJSONError(w, err)
			}
			return
		}
		s.writeJSON(w, apiV1SessionCreateResponse{SessionID: sess.ID, Activity: name})
	default:
		w.WriteHeader(405)
	}
}

// handleAPIv1Session — PATCH update / DELETE remove one session.
func (s *Server) handleAPIv1Session(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(400)
		s.writeJSON(w, apiErrorResponse{Error: "bad id"})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		updated, err := s.applySessionUpdateRequest(r)
		if err != nil {
			s.writeSessionUpdateError(w, r, err)
			return
		}
		s.writeJSON(w, apiV1SessionUpdateResponse{Updated: updated})
	case http.MethodDelete:
		if err := s.services.TrackingOps.Delete(operationContext(r), appmodel.SessionDeleteRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), SessionID: id}); err != nil {
			if s.writeSessionLockConflict(w, r, err) {
				return
			}
			if errors.Is(err, model.ErrNotFound) {
				s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "session not found"})
				return
			}
			s.writeInternalJSONError(w, err)
			return
		}
		s.writeJSON(w, apiV1SessionDeleteResponse{Deleted: id})
	default:
		w.WriteHeader(405)
	}
}

// handleAPIv1Projects — GET list.
func (s *Server) handleAPIv1Projects(w http.ResponseWriter, r *http.Request) {
	list, err := s.services.Projects.Queries.List(r.Context(), teamID(r), r.URL.Query().Get("archived") == "1")
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, apiV1ProjectsResponse{Projects: projectsFor(r, list)})
}

// handleAPIv1Report — GET summary totals for a window.
func (s *Server) handleAPIv1Report(w http.ResponseWriter, r *http.Request) {
	from, to := rangeFromQuery(r)
	result, err := s.services.ReportBuilder.Build(r.Context(), appmodel.ReportBuildQuery{
		TeamID: teamID(r), From: from, To: to, Now: userNow(r),
		GroupBy: "activity", Labels: appmodel.ReportLabels{
			Uncategorized: i18n.T(resolveLang(r), "dash.uncategorized"),
		},
	})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	byActivity := make(map[string]int, len(result.Rows))
	for _, row := range result.Rows {
		byActivity[row.Key] = row.Seconds
	}
	s.writeJSON(w, apiV1ReportResponse{
		From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
		TotalSeconds: result.TotalSeconds, ByActivity: byActivity,
	})
}

func rangeFromQuery(r *http.Request) (time.Time, time.Time) {
	now := userNow(r)
	from := now.AddDate(0, 0, -30)
	to := now.Add(24 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1)
		}
	}
	return from, to
}

// Cursors are opaque to clients: base64 of "start|id".
func encodeSessionCursor(c model.SessionCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.Start + "|" + strconv.FormatInt(c.ID, 10)))
}

func decodeSessionCursor(s string) (*model.SessionCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	start, idStr, ok := strings.Cut(string(b), "|")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if !ok || err != nil {
		return nil, false
	}
	if _, err := time.Parse(time.RFC3339Nano, start); err != nil || id <= 0 {
		return nil, false
	}
	return &model.SessionCursor{Start: start, ID: id}, true
}
