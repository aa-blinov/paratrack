package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// taskWatcherView is one person who follows a task, on the task page (TASK-12).
type taskWatcherView struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	RoleLabel string `json:"role_label"`
}

// handleTaskWatch lets a member follow or stop following a task, and lets a manager add anyone or
// name a co-assignee (TASK-12).
func (s *Server) handleTaskWatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	user := authenticatedUserID(r)
	if raw := strings.TrimSpace(r.PostForm.Get("user_id")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			user = parsed
		}
	}
	role := strings.TrimSpace(r.PostForm.Get("role"))
	if role == "" {
		role = "watcher"
	}
	if err := s.services.Tasks.SetTaskWatcher(r.Context(), appmodel.TaskWatchRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id, UserID: user,
		Role: role, Watch: r.PostForm.Get("watch") == "1",
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

// taskWatcherViews lists who follows a task for its page.
func (s *Server) taskWatcherViews(r *http.Request, taskID int64) ([]taskWatcherView, bool, error) {
	watchers, err := s.services.Tasks.ListTaskWatchers(r.Context(), teamID(r), taskID)
	if err != nil {
		return nil, false, err
	}
	lang := i18n.Lang(resolveLang(r))
	out := make([]taskWatcherView, 0, len(watchers))
	watching := false
	for _, w := range watchers {
		label := i18n.T(lang, "tasks.watcher")
		if w.Role == "co-assignee" {
			label = i18n.T(lang, "tasks.coAssignee")
		}
		out = append(out, taskWatcherView{UserID: w.UserID, Name: w.Name, Role: w.Role, RoleLabel: label})
		if w.UserID == authenticatedUserID(r) {
			watching = true
		}
	}
	return out, watching, nil
}

// notifyTaskWatchers sends a push to everyone who follows a task, except the person who made the
// change. A failed push does not fail the change that caused it (TASK-12).
func (s *Server) notifyTaskWatchers(r *http.Request, taskID int64, title, body string) {
	ids, err := s.services.Tasks.TaskWatcherIDs(r.Context(), teamID(r), taskID)
	if err != nil {
		s.logInternalError(err)
		return
	}
	var others []int64
	for _, id := range ids {
		if id != authenticatedUserID(r) {
			others = append(others, id)
		}
	}
	if len(others) == 0 {
		return
	}
	if err := s.services.Push.NotifyUsers(r.Context(), appmodel.PushNotifyRequest{
		TeamID: teamID(r), UserIDs: others, Title: title, Body: body,
		URL: "/tasks/" + strconv.FormatInt(taskID, 10), Topic: appmodel.NotificationTopicTaskWatch,
	}); err != nil {
		s.logInternalError(err)
	}
}
