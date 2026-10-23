package web

import (
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// taskLinkView is a task named on the other end of a dependency (TASK-11).
type taskLinkView struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// handleTaskDependencyAdd makes a task wait for another one in its project.
func (s *Server) handleTaskDependencyAdd(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || taskID <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	dependsOn, _ := strconv.ParseInt(r.PostForm.Get("depends_on"), 10, 64)
	if err := s.services.Tasks.AddTaskDependency(r.Context(), appmodel.TaskDependencyRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: taskID, DependsOnID: dependsOn,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistOKResponse{OK: true})
}

// handleTaskDependencyDelete removes a wait: the task no longer waits for the other one.
func (s *Server) handleTaskDependencyDelete(w http.ResponseWriter, r *http.Request) {
	taskID, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	dependsOn, err2 := strconv.ParseInt(r.PathValue("dep"), 10, 64)
	if err1 != nil || err2 != nil || taskID <= 0 || dependsOn <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := s.services.Tasks.DeleteTaskDependency(r.Context(), appmodel.TaskDependencyRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: taskID, DependsOnID: dependsOn,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskChecklistOKResponse{OK: true})
}

// dependencyViews lists what a task waits for, what it blocks, the tasks it could still wait
// for, and whether a task it waits for is not done yet (TASK-11).
func (s *Server) dependencyViews(r *http.Request, task taskItem) (waits, blocks, options []taskLinkView, blocked bool, err error) {
	waitLinks, blockLinks, err := s.services.Tasks.TaskDependencies(r.Context(), teamID(r), task.ID)
	if err != nil {
		return nil, nil, nil, false, err
	}
	waits = linkViews(waitLinks)
	blocks = linkViews(blockLinks)
	for _, link := range waitLinks {
		if !link.Done {
			blocked = true
		}
	}
	linked := make(map[int64]bool, len(waitLinks))
	for _, link := range waitLinks {
		linked[link.ID] = true
	}
	items, err := s.taskItems(r, appmodel.TaskQuery{ProjectID: task.ProjectID})
	if err != nil {
		return nil, nil, nil, false, err
	}
	options = []taskLinkView{}
	for _, item := range items {
		if item.ID == task.ID || linked[item.ID] || item.Archived {
			continue
		}
		options = append(options, taskLinkView{ID: item.ID, Title: item.Title, Done: item.Status == "done"})
	}
	return waits, blocks, options, blocked, nil
}

func linkViews(links []appmodel.TaskLink) []taskLinkView {
	out := make([]taskLinkView, 0, len(links))
	for _, link := range links {
		out = append(out, taskLinkView{ID: link.ID, Title: link.Title, Done: link.Done})
	}
	return out
}
