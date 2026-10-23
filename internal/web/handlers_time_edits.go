package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// timeEditView is one request on the timesheet page (TIME-19).
type timeEditView struct {
	ID       int64  `json:"id"`
	Person   string `json:"person"`
	Activity string `json:"activity"`
	Day      string `json:"day"`
	Minutes  int    `json:"minutes"`
	Hours    string `json:"hours"`
	Reason   string `json:"reason"`
	Status   string `json:"status"`
	Note     string `json:"note"`
	Mine     bool   `json:"mine"`
}

type timeEditCreatedResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

func (timeEditCreatedResponse) isJSONResponse() {}

type timeEditOKResponse struct {
	OK bool `json:"ok"`
}

func (timeEditOKResponse) isJSONResponse() {}

// timeEditViews builds the requests the viewer may see, for the timesheet page.
func (s *Server) timeEditViews(r *http.Request) ([]timeEditView, error) {
	requests, err := s.services.Tracking.Queries.TimeEditRequests(r.Context(), teamID(r), authenticatedUserID(r), canManage(r))
	if err != nil {
		return nil, err
	}
	out := make([]timeEditView, 0, len(requests))
	for _, q := range requests {
		out = append(out, timeEditView{
			ID: q.ID, Person: q.Person, Activity: q.ActivityName, Day: q.Day.Format("2006-01-02"),
			Minutes: q.Minutes, Hours: fmtDur(r, q.Minutes*60), Reason: q.Reason, Status: q.Status,
			Note: q.DecisionNote, Mine: q.UserID == authenticatedUserID(r),
		})
	}
	return out, nil
}

// handleTimeEditCreate records a member's request to change one of their timesheet cells (TIME-19).
func (s *Server) handleTimeEditCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	activityID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("activity_id")), 10, 64)
	day, err := time.Parse("2006-01-02", strings.TrimSpace(r.PostForm.Get("day")))
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid day"})
		return
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("minutes")))
	if err != nil || minutes < 0 || minutes > 24*60 {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid minutes"})
		return
	}
	created, err := s.services.Tracking.Commands.RequestTimeEdit(r.Context(), appmodel.TimeEditCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityID: activityID, Day: day,
		Minutes: minutes, Reason: r.PostForm.Get("reason"),
	})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, timeEditCreatedResponse{ID: created.ID, Status: created.Status})
}

// handleTimeEditDecision lets a manager approve or refuse a pending request (TIME-19).
func (s *Server) handleTimeEditDecision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	if err := s.services.Tracking.Commands.DecideTimeEdit(r.Context(), appmodel.TimeEditDecisionRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), RequestID: id,
		Approve: r.PostForm.Get("approve") == "1", Note: r.PostForm.Get("note"),
	}); err != nil {
		s.writeTimeEditError(w, r, err)
		return
	}
	s.writeJSON(w, timeEditOKResponse{OK: true})
}

// writeTimeEditError names why a decision was refused. A cell in an approved week is not written
// until the week is reopened, and the message says so (TIME-19).
func (s *Server) writeTimeEditError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrWeekApproved) {
		s.writeJSONStatus(w, http.StatusUnprocessableEntity, apiErrorResponse{Error: i18n.T(resolveLang(r), "tse.weekApproved")})
		return
	}
	s.writeTaskError(w, r, err)
}
