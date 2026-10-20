package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// taskItem is one task as the board, the table and the task card show it.
type taskItem struct {
	ID            int64
	Title         string
	Status        string
	ProjectID     int64
	ProjectName   string
	ProjectColor  string
	AssigneeID    int64
	AssigneeName  string
	Due           string
	DueLabel      string
	Overdue       bool
	EstimateMin   int
	EstimateLabel string
	TrackedMin    int
	TrackedLabel  string
	Activities    []taskActivity
	CanArchive    bool
	Archived      bool
	ExternalURL   string
}

type taskActivity struct {
	ID    int64
	Name  string
	Color string
}

// memberOption is one person in an assignee picker.
type taskPerson struct {
	ID   int64
	Name string
}

type tasksPage struct {
	pageData
	TasksReact bool
	View       string
	ProjectID  int64
	Projects   []projectView
	People     []taskPerson
	Tasks      []taskItem
	ViewerID   int64
	CanImport  bool
	Flash      string
	FlashOK    bool
}

func (p *tasksPage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *tasksPage) usesReactApp() bool { return p.TasksReact }

type taskDetailPage struct {
	pageData
	TaskDetailReact bool
	Task            taskItem
	Projects        []projectView
	People          []taskPerson
	Linkable        []taskActivity
	ViewerID        int64
}

func (p *taskDetailPage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *taskDetailPage) usesReactApp() bool { return p.TaskDetailReact }

type tasksListResponse struct {
	Tasks []taskItem `json:"tasks"`
}

func (tasksListResponse) isJSONResponse() {}

type taskCreatedResponse struct {
	ID int64 `json:"id"`
}

func (taskCreatedResponse) isJSONResponse() {}

type taskOKResponse struct {
	OK bool `json:"ok"`
}

func (taskOKResponse) isJSONResponse() {}

type taskActivityResponse struct {
	Activity taskActivity `json:"activity"`
}

func (taskActivityResponse) isJSONResponse() {}

type taskLinkableResponse struct {
	Activities []taskActivity `json:"activities"`
}

func (taskLinkableResponse) isJSONResponse() {}

// taskPeople lists every member, for an assignee picker. Anyone on the team
// may assign a task, so this is not limited to managers.
func (s *Server) taskPeople(r *http.Request) []taskPerson {
	members, err := s.services.Teams.Directory.Members(r.Context(), teamID(r))
	if err != nil {
		return nil
	}
	out := make([]taskPerson, 0, len(members))
	for _, member := range members {
		name := member.Name
		if name == "" {
			name = member.Email
		}
		out = append(out, taskPerson{ID: member.UserID, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// taskItems builds the tasks a query asks for: the open board or the archive,
// for the whole workspace or one project, or a single task whether it is
// archived or not.
func (s *Server) taskItems(r *http.Request, query appmodel.TaskQuery) ([]taskItem, error) {
	now := userNow(r)
	query.TeamID = teamID(r)
	query.Now = now
	rows, err := s.services.Tasks.List(r.Context(), query)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, person := range s.taskPeople(r) {
		names[person.ID] = person.Name
	}
	viewer := authenticatedUserID(r)
	manage := canManage(r)
	today := startOfDay(now)
	items := make([]taskItem, 0, len(rows))
	for _, row := range rows {
		task := row.Task
		item := taskItem{
			ID: task.ID, Title: task.Title, Status: task.Status, Archived: task.Archived,
			ProjectID: task.ProjectID, ProjectName: row.ProjectName, ProjectColor: row.ProjectColor,
			AssigneeID: task.AssigneeID, AssigneeName: names[task.AssigneeID],
			TrackedMin: row.TrackedMin, TrackedLabel: fmtDur(r, row.TrackedMin*60),
			CanArchive:  manage || (task.CreatedBy > 0 && task.CreatedBy == viewer),
			ExternalURL: task.ExternalURL,
		}
		if task.Due != "" {
			if day, err := time.ParseInLocation("2006-01-02", task.Due, userLoc(r)); err == nil {
				item.Due = task.Due
				item.DueLabel = fmtDate(resolveLang(r), day)
				item.Overdue = task.Status != model.TaskStatusDone && day.Before(today)
			}
		}
		if task.EstimateMinutes > 0 {
			item.EstimateMin = task.EstimateMinutes
			item.EstimateLabel = fmtDur(r, task.EstimateMinutes*60)
		}
		for _, a := range row.Activities {
			item.Activities = append(item.Activities, taskActivity{ID: a.ID, Name: a.Name, Color: a.Color})
		}
		items = append(items, item)
	}
	return items, nil
}

// handleTasks renders the task board: a table by default, the columns, or the
// archive. The archive holds the tasks taken off the board; they stay readable
// there and can be put back.
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	if view != "board" && view != "archive" {
		view = "table"
	}
	projectID, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	items, err := s.taskItems(r, appmodel.TaskQuery{ProjectID: projectID, Archived: view == "archive"})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := tasksPage{
		pageData:   pageData{Title: "Tasks", Active: "tasks", Lang: string(resolveLang(r)), ReactApp: true},
		TasksReact: true,
		View:       view,
		ProjectID:  projectID,
		Projects:   s.activeProjectViews(r),
		People:     s.taskPeople(r),
		Tasks:      items,
		ViewerID:   authenticatedUserID(r),
		CanImport:  canManage(r),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Tasks", "tasks", &data)
}

// handleTaskDetail renders one task: its fields, its activities, and the
// activities of its project that it can still take on. An archived task still
// opens, read-only: it takes no new activities and can be put back on the board.
func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	items, err := s.taskItems(r, appmodel.TaskQuery{TaskID: id})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	if len(items) == 0 {
		http.NotFound(w, r)
		return
	}
	task := items[0]
	linkable := []taskActivity{}
	if !task.Archived {
		list, err := s.services.Tasks.Linkable(r.Context(), appmodel.TaskQuery{TeamID: teamID(r), ProjectID: task.ProjectID})
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		linkable = taskActivities(list)
	}
	data := taskDetailPage{
		pageData:        pageData{Title: task.Title, Active: "tasks", Lang: string(resolveLang(r)), ReactApp: true},
		TaskDetailReact: true,
		Task:            task,
		Projects:        s.activeProjectViews(r),
		People:          s.taskPeople(r),
		Linkable:        linkable,
		ViewerID:        authenticatedUserID(r),
	}
	s.renderPageForRequest(w, r, task.Title, "tasks", &data)
}

func taskActivities(list []appmodel.Activity) []taskActivity {
	out := make([]taskActivity, 0, len(list))
	for _, a := range list {
		out = append(out, taskActivity{ID: a.ID, Name: a.Name, Color: a.Color})
	}
	return out
}

func (s *Server) activeProjectViews(r *http.Request) []projectView {
	projects, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r)})
	if err != nil {
		return nil
	}
	return projectViews(projects)
}

func (s *Server) handleTasksAPIList(w http.ResponseWriter, r *http.Request) {
	projectID, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	items, err := s.taskItems(r, appmodel.TaskQuery{ProjectID: projectID, Archived: r.URL.Query().Get("archived") == "1"})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, tasksListResponse{Tasks: items})
}

func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	projectID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("project_id")), 10, 64)
	assignee, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("assignee_id")), 10, 64)
	estimate, _ := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("estimate_minutes")))
	task, err := s.services.Tasks.Create(r.Context(), appmodel.TaskCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ProjectID: projectID,
		Title: r.PostForm.Get("title"), Status: strings.TrimSpace(r.PostForm.Get("status")),
		AssigneeID: assignee, Due: strings.TrimSpace(r.PostForm.Get("due")), EstimateMinutes: estimate,
	})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskCreatedResponse{ID: task.ID})
}

// handleTaskUpdate changes the fields the form sent; a missing field is kept.
func (s *Server) handleTaskUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	request := appmodel.TaskUpdateRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id}
	if _, sent := r.PostForm["title"]; sent {
		title := r.PostForm.Get("title")
		request.Title = &title
	}
	if _, sent := r.PostForm["assignee_id"]; sent {
		assignee, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("assignee_id")), 10, 64)
		request.AssigneeID = &assignee
	}
	if _, sent := r.PostForm["due"]; sent {
		due := strings.TrimSpace(r.PostForm.Get("due"))
		request.Due = &due
	}
	if _, sent := r.PostForm["estimate_minutes"]; sent {
		estimate, _ := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("estimate_minutes")))
		request.EstimateMinutes = &estimate
	}
	if _, err := s.services.Tasks.Update(r.Context(), request); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.services.Tasks.SetStatus(r.Context(), appmodel.TaskStatusRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id, Status: strings.TrimSpace(r.FormValue("status")),
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

func (s *Server) handleTaskArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.services.Tasks.Archive(r.Context(), appmodel.TaskArchiveRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

// handleTaskRestore puts an archived task back on the board, at the end of its column.
func (s *Server) handleTaskRestore(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.services.Tasks.Restore(r.Context(), appmodel.TaskRestoreRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

// handleTaskActivity hangs an activity on a task: a new one by name, or one
// the task's project already has that no task holds.
func (s *Server) handleTaskActivity(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	activityID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("activity_id")), 10, 64)
	activity, err := s.services.Tasks.LinkActivity(r.Context(), appmodel.TaskActivityRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id,
		ActivityID: activityID, Name: r.PostForm.Get("name"),
	})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskActivityResponse{Activity: taskActivity{ID: activity.ID, Name: activity.Name, Color: activity.Color}})
}

func (s *Server) handleTaskLinkable(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	items, err := s.taskItems(r, appmodel.TaskQuery{TaskID: id})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	if len(items) == 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if items[0].Archived {
		s.writeJSON(w, taskLinkableResponse{Activities: []taskActivity{}})
		return
	}
	list, err := s.services.Tasks.Linkable(r.Context(), appmodel.TaskQuery{TeamID: teamID(r), ProjectID: items[0].ProjectID})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.writeJSON(w, taskLinkableResponse{Activities: taskActivities(list)})
}

func (s *Server) taskIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "bad task id"})
		return 0, false
	}
	return id, true
}

// writeTaskError maps a task failure to the status and message a form can show.
func (s *Server) writeTaskError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, appmodel.ErrTaskNotFound), errors.Is(err, model.ErrNotFound):
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
	case errors.Is(err, appmodel.ErrTaskActivityTaken):
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "tasks.activityTaken")})
	case errors.Is(err, appmodel.ErrTaskArchived):
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "tasks.archivedReadOnly")})
	case errors.Is(err, appmodel.ErrInvalidTask):
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
	default:
		s.writeInternalJSONError(w, fmt.Errorf("task: %w", err))
	}
}

// handleTasksImportGitHub moves the items of a GitHub Projects board into a
// project's tasks and answers with a flash on the board. The token serves this
// request only: it is not stored, logged or shown back.
func (s *Server) handleTasksImportGitHub(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/tasks?flash="+url.QueryEscape(encodeFlash(false, i18n.T(resolveLang(r), "err.invalidInput"))), http.StatusSeeOther)
		return
	}
	projectID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("project_id")), 10, 64)
	back := "/tasks?view=table"
	if projectID > 0 {
		back += "&project=" + strconv.FormatInt(projectID, 10)
	}
	result, err := s.services.Tasks.ImportGitHubProject(r.Context(), appmodel.GitHubImportRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ProjectID: projectID,
		ProjectURL: r.PostForm.Get("project_url"), Token: r.PostForm.Get("token"),
	})
	var flash string
	if err != nil {
		flash = encodeFlash(false, s.githubImportMessage(r, err))
	} else {
		flash = encodeFlash(true, fmt.Sprintf(i18n.T(resolveLang(r), "tasks.githubDone"),
			result.Created, result.Updated, result.Unchanged, result.Archived))
	}
	http.Redirect(w, r, back+"&flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

// githubImportMessage says why an import failed, in the words a manager can act on.
func (s *Server) githubImportMessage(r *http.Request, err error) string {
	lang := resolveLang(r)
	switch {
	case errors.Is(err, appmodel.ErrGitHubProjectURL):
		return i18n.T(lang, "tasks.githubBadURL")
	case errors.Is(err, appmodel.ErrGitHubProjectNotFound):
		return i18n.T(lang, "tasks.githubNotFound")
	case errors.Is(err, appmodel.ErrGitHubTokenRejected):
		return i18n.T(lang, "tasks.githubTokenRejected")
	case errors.Is(err, appmodel.ErrGitHubProjectTooLarge):
		return i18n.T(lang, "tasks.githubTooLarge")
	case errors.Is(err, appmodel.ErrInvalidTask), errors.Is(err, model.ErrForbidden):
		return i18n.T(lang, "err.invalidInput")
	default:
		s.logInternalError(err)
		return i18n.T(lang, "err.internal")
	}
}
