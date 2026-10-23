package web

import (
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// taskChecklistView is one step of a task's checklist on the task page (TASK-10).
type taskChecklistView struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	Done bool   `json:"done"`
}

type taskChecklistItemResponse struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	Done bool   `json:"done"`
}

func (taskChecklistItemResponse) isJSONResponse() {}

type taskChecklistOKResponse struct {
	OK bool `json:"ok"`
}

func (taskChecklistOKResponse) isJSONResponse() {}

// handleTaskChecklistAdd appends a step to a task's checklist.
func (s *Server) handleTaskChecklistAdd(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	item, err := s.services.Tasks.AddChecklistItem(r.Context(), appmodel.TaskChecklistAddRequest{
		TeamID: teamID(r), TaskID: id, CallerID: authenticatedUserID(r), Body: r.PostForm.Get("body"),
	})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistItemResponse{ID: item.ID, Body: item.Body, Done: item.Done})
}

// handleTaskChecklistSet marks one step of a task's checklist done or not again.
func (s *Server) handleTaskChecklistSet(w http.ResponseWriter, r *http.Request) {
	taskID, itemID, ok := s.taskChecklistIDs(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	if err := s.services.Tasks.SetChecklistItem(r.Context(), appmodel.TaskChecklistSetRequest{
		TeamID: teamID(r), TaskID: taskID, CallerID: authenticatedUserID(r), ItemID: itemID, Done: r.PostForm.Get("done") == "1",
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistOKResponse{OK: true})
}

// handleTaskChecklistDelete removes one step of a task's checklist.
func (s *Server) handleTaskChecklistDelete(w http.ResponseWriter, r *http.Request) {
	taskID, itemID, ok := s.taskChecklistIDs(w, r)
	if !ok {
		return
	}
	if err := s.services.Tasks.DeleteChecklistItem(r.Context(), appmodel.TaskChecklistDeleteRequest{
		TeamID: teamID(r), TaskID: taskID, CallerID: authenticatedUserID(r), ItemID: itemID,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistOKResponse{OK: true})
}

// taskChecklistIDs reads the task and the step from the path; a bad id answers 404.
func (s *Server) taskChecklistIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	taskID, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	itemID, err2 := strconv.ParseInt(r.PathValue("item"), 10, 64)
	if err1 != nil || err2 != nil || taskID <= 0 || itemID <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return 0, 0, false
	}
	return taskID, itemID, true
}
