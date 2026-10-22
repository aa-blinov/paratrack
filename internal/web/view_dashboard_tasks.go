package web

import (
	"net/http"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// dashboardTask is an open task offered on the start form (TIME-04). Choosing it
// starts the time on the task's own activity.
type dashboardTask struct {
	ID          int64
	Title       string
	ProjectID   int64
	ProjectName string
}

// openTaskOptions lists the workspace's open tasks for the start form. A failure to
// read them leaves the list empty; the free-text activity still works.
func (s *Server) openTaskOptions(r *http.Request) []dashboardTask {
	rows, err := s.services.Tasks.List(r.Context(), appmodel.TaskQuery{TeamID: teamID(r)})
	if err != nil {
		s.logInternalError(err)
		return nil
	}
	out := make([]dashboardTask, 0, len(rows))
	for _, row := range rows {
		if row.Task.Archived || row.Task.Status == model.TaskStatusDone {
			continue
		}
		out = append(out, dashboardTask{ID: row.Task.ID, Title: row.Task.Title, ProjectID: row.Task.ProjectID, ProjectName: row.ProjectName})
	}
	return out
}
