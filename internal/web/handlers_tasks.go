package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// taskItem is one task as the board, the table and the task card show it.
type taskItem struct {
	ID           int64
	Title        string
	Status       string
	ProjectID    int64
	ProjectName  string
	ProjectColor string
	AssigneeID   int64
	AssigneeName string
	// AssigneeGone is set when the task still names an assignee who is no longer
	// in the team: the reference stays, so the task is not an unassigned one.
	AssigneeGone  bool
	Due           string
	DueLabel      string
	Overdue       bool
	EstimateMin   int
	EstimateLabel string
	Description   string
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
	// The task report (TASK-04): managers only, for a period of days.
	CanReport   bool
	ReportFrom  string
	ReportTo    string
	ReportNote  string
	ReportRows  []taskReportRow
	ReportTotal string
}

// taskReportRow is one task in the period report: its time inside the period, and
// the attributes the report shows next to it.
type taskReportRow struct {
	Title         string
	ProjectName   string
	AssigneeName  string
	AssigneeGone  bool
	Status        string
	EstimateMin   int
	EstimateLabel string
	Due           string
	DueLabel      string
	Archived      bool
	Seconds       int
	TrackedLabel  string
}

func (p *tasksPage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *tasksPage) usesReactApp() bool { return p.TasksReact }

type taskDetailPage struct {
	pageData
	TaskDetailReact   bool
	Task              taskItem
	Projects          []projectView
	People            []taskPerson
	Linkable          []taskActivity
	ViewerID          int64
	Comments          []taskCommentView
	Checklist         []taskChecklistView
	Subtasks          []taskSubtaskView
	SubtaskRollup     string
	Attachments       []taskAttachmentView
	Watchers          []taskWatcherView
	Watching          bool
	WaitsFor          []taskLinkView
	Blocks            []taskLinkView
	DependencyOptions []taskLinkView
	Blocked           bool
}

// taskCommentView is one line of a task's feed as the page shows it.
type taskCommentView struct {
	ID     int64
	Author string
	Body   string
	When   string
}

// taskCommentResponse answers a comment that was written, as the feed shows it.
type taskCommentResponse struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	When   string `json:"when"`
}

func (taskCommentResponse) isJSONResponse() {}

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
			AssigneeGone: task.AssigneeID > 0 && names[task.AssigneeID] == "",
			TrackedMin:   row.TrackedMin, TrackedLabel: fmtDur(r, row.TrackedMin*60),
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
		item.Description = task.Description
		if task.EstimateMinutes > 0 {
			item.EstimateMin = task.EstimateMinutes
			item.EstimateLabel = fmtDurF(resolveLang(r), "clock", task.EstimateMinutes*60)
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
	// The report is for managers; anyone else gets the table.
	if view != "board" && view != "archive" && !(view == "report" && canManage(r)) {
		view = "table"
	}
	projectID, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	items := []taskItem{} // never null: the page reads it as a list
	if view != "report" {
		var err error
		items, err = s.taskItems(r, appmodel.TaskQuery{ProjectID: projectID, Archived: view == "archive"})
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
	}
	data := tasksPage{
		pageData:   pageData{Title: i18n.T(resolveLang(r), "nav.tasks"), Active: "tasks", Lang: string(resolveLang(r)), ReactApp: true, CanManage: canManage(r)},
		TasksReact: true,
		View:       view,
		ProjectID:  projectID,
		Projects:   s.activeProjectViews(r),
		People:     s.taskPeople(r),
		Tasks:      items,
		ViewerID:   authenticatedUserID(r),
		CanImport:  canManage(r),
		CanReport:  canManage(r),
	}
	if view == "report" {
		if err := s.fillTaskReport(r, &data); err != nil {
			s.writeInternalError(w, err)
			return
		}
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, data.Title, "tasks", &data)
}

// fillTaskReport sets the task report for the period in the query (TASK-04). Both
// days are included, in the viewer's zone. Without a period, or with one that ends
// before it starts, the report shows the last seven days or says why it does not.
func (s *Server) fillTaskReport(r *http.Request, data *tasksPage) error {
	loc := userLoc(r)
	today := startOfDay(userNow(r))
	from, to := today.AddDate(0, 0, -6), today
	if day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("from"), loc); err == nil {
		from = day
	}
	if day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("to"), loc); err == nil {
		to = day
	}
	data.ReportFrom, data.ReportTo = from.Format("2006-01-02"), to.Format("2006-01-02")
	data.ReportRows = []taskReportRow{} // never null: the page reads it as a list
	if to.Before(from) {
		data.ReportNote = i18n.T(resolveLang(r), "tasks.reportBadPeriod")
		return nil
	}
	rows, total, err := s.taskReport(r, from, to.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	data.ReportRows = rows
	data.ReportTotal = fmtDur(r, total)
	return nil
}

// taskReport lists the tasks with time inside [start, end), sorted by project and
// title, and the total of their time. An archived task stays in: archiving keeps
// its time, so it keeps its line.
func (s *Server) taskReport(r *http.Request, start, end time.Time) ([]taskReportRow, int, error) {
	seconds, err := s.services.Tasks.TrackedSecondsByTask(r.Context(), appmodel.TaskReportQuery{
		TeamID: teamID(r), Start: start, End: end, Now: userNow(r),
	})
	if err != nil {
		return nil, 0, err
	}
	rows := []taskReportRow{}
	if len(seconds) == 0 {
		return rows, 0, nil
	}
	active, err := s.taskItems(r, appmodel.TaskQuery{})
	if err != nil {
		return nil, 0, err
	}
	archived, err := s.taskItems(r, appmodel.TaskQuery{Archived: true})
	if err != nil {
		return nil, 0, err
	}
	total := 0
	for _, item := range append(active, archived...) {
		tracked := seconds[item.ID]
		if tracked <= 0 {
			continue
		}
		total += tracked
		rows = append(rows, taskReportRow{
			Title: item.Title, ProjectName: item.ProjectName, AssigneeName: item.AssigneeName, AssigneeGone: item.AssigneeGone,
			Status: item.Status, EstimateMin: item.EstimateMin, EstimateLabel: item.EstimateLabel,
			Due: item.Due, DueLabel: item.DueLabel, Archived: item.Archived,
			Seconds: tracked, TrackedLabel: fmtDur(r, tracked),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ProjectName != rows[j].ProjectName {
			return rows[i].ProjectName < rows[j].ProjectName
		}
		return rows[i].Title < rows[j].Title
	})
	return rows, total, nil
}

// handleTaskReportCSV downloads the task report for the period in the query (TASK-04).
func (s *Server) handleTaskReportCSV(w http.ResponseWriter, r *http.Request) {
	loc := userLoc(r)
	from, errFrom := time.ParseInLocation("2006-01-02", r.URL.Query().Get("from"), loc)
	to, errTo := time.ParseInLocation("2006-01-02", r.URL.Query().Get("to"), loc)
	if errFrom != nil || errTo != nil || to.Before(from) {
		http.Error(w, i18n.T(resolveLang(r), "export.invalidPeriod"), http.StatusBadRequest)
		return
	}
	rows, _, err := s.taskReport(r, from, to.AddDate(0, 0, 1))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tasks-%s-%s.csv"`,
		from.Format("2006-01-02"), to.Format("2006-01-02")))
	if err := writeTaskReportCSV(w, rows); err != nil {
		s.logInternalError(err)
	}
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
	waits, blocks, options, blocked, err := s.dependencyViews(r, task)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	watchers, watching, err := s.taskWatcherViews(r, id)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	attachments, err := s.taskAttachmentViews(r, id)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	subtasks, rollup, err := s.subtaskViews(r, task)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	steps, err := s.services.Tasks.Checklist(r.Context(), teamID(r), id)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	checklist := make([]taskChecklistView, 0, len(steps))
	for _, step := range steps {
		checklist = append(checklist, taskChecklistView{ID: step.ID, Body: step.Body, Done: step.Done})
	}
	comments, err := s.services.Tasks.Comments(r.Context(), teamID(r), id)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	feed := make([]taskCommentView, 0, len(comments))
	for _, c := range comments {
		feed = append(feed, taskCommentView{ID: c.ID, Author: c.Author, Body: c.Body, When: fmtWhen(resolveLang(r), c.CreatedAt.In(userLoc(r)), userNow(r))})
	}
	data := taskDetailPage{
		Comments:          feed,
		Checklist:         checklist,
		Subtasks:          subtasks,
		SubtaskRollup:     rollup,
		Attachments:       attachments,
		Watchers:          watchers,
		Watching:          watching,
		WaitsFor:          waits,
		Blocks:            blocks,
		DependencyOptions: options,
		Blocked:           blocked,
		pageData:          pageData{Title: task.Title, Active: "tasks", Lang: string(resolveLang(r)), ReactApp: true},
		TaskDetailReact:   true,
		Task:              task,
		Projects:          s.activeProjectViews(r),
		People:            s.taskPeople(r),
		Linkable:          linkable,
		ViewerID:          authenticatedUserID(r),
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

// taskEstimateMinutes reads a task estimate written as hours and minutes ("1:30"),
// as the forms show it. Plain minutes ("90") still parse, so older clients keep
// working. Empty means no estimate.
func taskEstimateMinutes(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	seconds, err := timeparse.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return seconds / 60, nil
}

func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	projectID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("project_id")), 10, 64)
	assignee, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("assignee_id")), 10, 64)
	estimate, err := taskEstimateMinutes(r.PostForm.Get("estimate_minutes"))
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid estimate"})
		return
	}
	parentID, _ := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("parent_id")), 10, 64)
	task, err := s.services.Tasks.Create(r.Context(), appmodel.TaskCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ProjectID: projectID, ParentID: parentID,
		Title: r.PostForm.Get("title"), Status: strings.TrimSpace(r.PostForm.Get("status")),
		AssigneeID: assignee, Due: strings.TrimSpace(r.PostForm.Get("due")), EstimateMinutes: estimate,
		Description: strings.TrimSpace(r.PostForm.Get("description")),
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
		estimate, err := taskEstimateMinutes(r.PostForm.Get("estimate_minutes"))
		if err != nil {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid estimate"})
			return
		}
		request.EstimateMinutes = &estimate
	}
	if _, sent := r.PostForm["description"]; sent {
		description := strings.TrimSpace(r.PostForm.Get("description"))
		request.Description = &description
	}
	var before []appmodel.TaskRow
	if _, sent := r.PostForm["assignee_id"]; sent && r.PostForm.Get("move_plan") == "1" {
		before, _ = s.services.Tasks.List(r.Context(), appmodel.TaskQuery{TeamID: teamID(r), TaskID: id})
	}
	if _, err := s.services.Tasks.Update(r.Context(), request); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	if len(before) > 0 && request.AssigneeID != nil && *request.AssigneeID != before[0].Task.AssigneeID && before[0].Task.AssigneeID > 0 {
		// The plan follows the task only when the person asked for it (TASK-03).
		if _, err := s.services.Scheduling.MovePlanFromDay(r.Context(), appmodel.PlanMoveRequest{
			TeamID: teamID(r), ProjectID: before[0].Task.ProjectID, FromUserID: before[0].Task.AssigneeID, ToUserID: *request.AssigneeID,
			CallerID: authenticatedUserID(r), From: startOfDay(userNow(r)),
		}); err != nil {
			if errors.Is(err, model.ErrForbidden) {
				s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: i18n.T(resolveLang(r), "tasks.planMoveForbidden")})
				return
			}
			s.writeInternalJSONError(w, fmt.Errorf("move plan: %w", err))
			return
		}
	}
	s.writeJSON(w, taskOKResponse{OK: true})
}

// taskPlanPreview says how much plan would move with a task to another person (TASK-03).
type taskPlanPreview struct {
	Minutes int    `json:"minutes"`
	Hours   string `json:"hours"`
	CanMove bool   `json:"can_move"`
}

func (taskPlanPreview) isJSONResponse() {}

// handleTaskPlanMove answers before a reassignment is saved: the plan the current
// assignee still has on the task's project from today on (TASK-03).
func (s *Server) handleTaskPlanMove(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	to, _ := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	rows, err := s.services.Tasks.List(r.Context(), appmodel.TaskQuery{TeamID: teamID(r), TaskID: id})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	if len(rows) == 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	task := rows[0].Task
	minutes := 0
	if task.AssigneeID > 0 && to > 0 && to != task.AssigneeID {
		minutes, err = s.services.Scheduling.PlannedFromDayForUser(r.Context(), teamID(r), task.ProjectID, task.AssigneeID, startOfDay(userNow(r)))
		if err != nil {
			s.writeInternalJSONError(w, err)
			return
		}
	}
	// Who may move the plan is the same rule the write checks (MovePlanFromDay): a
	// manager, or the lead of the task's project.
	canMove := canManage(r) || s.leadsProject(r, task.ProjectID)
	s.writeJSON(w, taskPlanPreview{Minutes: minutes, Hours: fmtDur(r, minutes*60), CanMove: canMove})
}

// leadsProject reports whether the signed-in person leads the project. It only
// shapes the question shown before a reassignment; the write checks it again.
func (s *Server) leadsProject(r *http.Request, projectID int64) bool {
	viewer := authenticatedUserID(r)
	if viewer <= 0 {
		return false
	}
	list, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r), IncludeArchived: true})
	if err != nil {
		return false
	}
	for _, project := range list {
		if project.ID == projectID {
			return project.LeadUserID == viewer
		}
	}
	return false
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskIDFromPath(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.FormValue("status"))
	if err := s.services.Tasks.SetStatus(r.Context(), appmodel.TaskStatusRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: id, Status: status,
	}); err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.notifyTaskWatchers(r, id, i18n.T(resolveLang(r), "watch.statusTitle"), status)
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
	case errors.Is(err, appmodel.ErrTaskNameTaken):
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "tasks.nameTaken")})
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
		http.Redirect(w, r, "/tasks?flash="+encodeFlash(false, i18n.T(resolveLang(r), "err.invalidInput")), http.StatusSeeOther)
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
	http.Redirect(w, r, back+"&flash="+flash, http.StatusSeeOther)
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

// handleTaskComment appends a line to a task's feed (TASK-02).
func (s *Server) handleTaskComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	comment, err := s.services.Tasks.AddComment(r.Context(), appmodel.TaskCommentRequest{
		TeamID: teamID(r), TaskID: id, CallerID: authenticatedUserID(r), Body: r.PostForm.Get("body"),
	})
	if err != nil {
		s.writeTaskError(w, r, err)
		return
	}
	s.notifyTaskWatchers(r, id, i18n.T(resolveLang(r), "watch.commentTitle"), comment.Body)
	s.writeJSON(w, taskCommentResponse{ID: comment.ID, Author: comment.Author, Body: comment.Body,
		When: fmtWhen(resolveLang(r), comment.CreatedAt.In(userLoc(r)), userNow(r))})
}

// taskSubtaskView is one subtask in its parent's list (TASK-09).
type taskSubtaskView struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Done         bool   `json:"done"`
	TrackedLabel string `json:"tracked_label"`
}

// subtaskViews lists a task's subtasks with the time each logged, and the rollup line: how many
// are done, and the time on the task with its subtasks (TASK-09).
func (s *Server) subtaskViews(r *http.Request, task taskItem) ([]taskSubtaskView, string, error) {
	subs, err := s.services.Tasks.Subtasks(r.Context(), teamID(r), task.ID)
	if err != nil {
		return nil, "", err
	}
	if len(subs) == 0 {
		return []taskSubtaskView{}, "", nil
	}
	items, err := s.taskItems(r, appmodel.TaskQuery{ProjectID: task.ProjectID})
	if err != nil {
		return nil, "", err
	}
	tracked := make(map[int64]int, len(items))
	for _, item := range items {
		tracked[item.ID] = item.TrackedMin
	}
	out := make([]taskSubtaskView, 0, len(subs))
	done, total := 0, 0
	withSubtasks := tracked[task.ID]
	for _, sub := range subs {
		total++
		isDone := sub.Status == "done"
		if isDone {
			done++
		}
		withSubtasks += tracked[sub.ID]
		out = append(out, taskSubtaskView{
			ID: sub.ID, Title: sub.Title, Status: sub.Status, Done: isDone,
			TrackedLabel: fmtDur(r, tracked[sub.ID]*60),
		})
	}
	lang := resolveLang(r)
	rollup := fmt.Sprintf(i18n.T(lang, "tasks.subtaskRollup"), done, total, fmtDur(r, withSubtasks*60))
	return out, rollup, nil
}
