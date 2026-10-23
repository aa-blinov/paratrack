// Package tasks owns the work on a project: a board of tasks, each holding the
// activities its time is logged to.
package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Store is the persistence the task board needs. Authorisation is enforced
// there, in the same transaction as each write.
type Store interface {
	TaskOfActivity(ctx context.Context, teamID, activityID int64) (int64, error)
	StartTaskWork(ctx context.Context, request appmodel.TaskStartRequest) error
	AddTaskComment(ctx context.Context, request appmodel.TaskCommentRequest) (appmodel.TaskComment, error)
	ListTaskComments(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskComment, error)
	AddChecklistItem(ctx context.Context, request appmodel.TaskChecklistAddRequest) (appmodel.TaskChecklistItem, error)
	SetChecklistItem(ctx context.Context, request appmodel.TaskChecklistSetRequest) error
	DeleteChecklistItem(ctx context.Context, request appmodel.TaskChecklistDeleteRequest) error
	ListChecklist(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskChecklistItem, error)
	SubtasksOf(ctx context.Context, teamID, parentID int64) ([]model.Task, error)
	CreateTask(ctx context.Context, request appmodel.TaskCreateRequest) (model.Task, error)
	UpdateTask(ctx context.Context, request appmodel.TaskUpdateRequest) (model.Task, error)
	SetTaskStatus(ctx context.Context, request appmodel.TaskStatusRequest) error
	ArchiveTask(ctx context.Context, request appmodel.TaskArchiveRequest) error
	RestoreTask(ctx context.Context, request appmodel.TaskRestoreRequest) error
	LinkTaskActivity(ctx context.Context, request appmodel.TaskActivityRequest) (appmodel.Activity, error)
	ListTasks(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.TaskRow, error)
	ListLinkableActivities(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.Activity, error)
	ActivityHasTask(ctx context.Context, teamID, activityID int64) (bool, error)
	LinkedActivityTasks(ctx context.Context, teamID int64) (map[int64]appmodel.ActivityTask, error)
	TrackedSecondsByTask(ctx context.Context, query appmodel.TaskReportQuery) (map[int64]int, error)
	ImportTasks(ctx context.Context, request appmodel.TaskImportRequest) (appmodel.TaskImportResult, error)
}

// ProjectSource reads the items of a GitHub Projects board. The provider adapter
// implements it; the token is used for the one read and not kept.
type ProjectSource interface {
	FetchGitHubProject(ctx context.Context, token, projectURL string) ([]appmodel.GitHubProjectItem, error)
}

// Service validates what a person typed, then hands it to the store.
type Service struct {
	store  Store
	source ProjectSource
	now    func() time.Time
}

// New builds the task service. The source may be nil: the GitHub import then
// refuses to run. Now defaults to the wall clock.
func New(store Store, source ProjectSource, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("tasks store is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, source: source, now: now}, nil
}

// Statuses lists the board columns in order.
var Statuses = []string{model.TaskStatusTodo, model.TaskStatusDoing, model.TaskStatusDone}

// ValidStatus reports whether a status is a board column.
func ValidStatus(status string) bool {
	for _, known := range Statuses {
		if status == known {
			return true
		}
	}
	return false
}

const maxTitle = 200

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > maxTitle {
		return "", appmodel.ErrInvalidTask
	}
	return title, nil
}

// Create adds a task with its first activity. A new task starts in «к работе»
// unless a column is asked for.
func (s *Service) Create(ctx context.Context, request appmodel.TaskCreateRequest) (model.Task, error) {
	if request.TeamID <= 0 {
		return model.Task{}, appmodel.ErrInvalidTask
	}
	title, err := cleanTitle(request.Title)
	if err != nil {
		return model.Task{}, err
	}
	request.Title = title
	if request.Status == "" {
		request.Status = model.TaskStatusTodo
	}
	if !ValidStatus(request.Status) || request.EstimateMinutes < 0 {
		return model.Task{}, appmodel.ErrInvalidTask
	}
	return s.store.CreateTask(ctx, request)
}

// Update changes the fields a form sent.
func (s *Service) Update(ctx context.Context, request appmodel.TaskUpdateRequest) (model.Task, error) {
	if request.TeamID <= 0 {
		return model.Task{}, appmodel.ErrInvalidTask
	}
	if request.Title != nil {
		title, err := cleanTitle(*request.Title)
		if err != nil {
			return model.Task{}, err
		}
		request.Title = &title
	}
	return s.store.UpdateTask(ctx, request)
}

// SetStatus moves a task to another column.
func (s *Service) SetStatus(ctx context.Context, request appmodel.TaskStatusRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	if !ValidStatus(request.Status) {
		return appmodel.ErrInvalidTask
	}
	return s.store.SetTaskStatus(ctx, request)
}

// Archive takes a task off the board.
func (s *Service) Archive(ctx context.Context, request appmodel.TaskArchiveRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	return s.store.ArchiveTask(ctx, request)
}

// Restore puts an archived task back on the board.
func (s *Service) Restore(ctx context.Context, request appmodel.TaskRestoreRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	return s.store.RestoreTask(ctx, request)
}

// LinkActivity hangs an activity on a task.
// ActivityHasTask reports whether time logged to the activity is on a task.
func (s *Service) ActivityHasTask(ctx context.Context, teamID, activityID int64) (bool, error) {
	if teamID <= 0 {
		return false, appmodel.ErrInvalidTask
	}
	return s.store.ActivityHasTask(ctx, teamID, activityID)
}

// TaskOfActivity returns the task an activity belongs to, or zero (TIME-04).
func (s *Service) TaskOfActivity(ctx context.Context, teamID, activityID int64) (int64, error) {
	if teamID <= 0 {
		return 0, appmodel.ErrInvalidTask
	}
	return s.store.TaskOfActivity(ctx, teamID, activityID)
}

// StartWork moves a task to "in progress" when time starts on it (TIME-04).
func (s *Service) StartWork(ctx context.Context, request appmodel.TaskStartRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	return s.store.StartTaskWork(ctx, request)
}

// AddComment writes a line to a task's feed (TASK-02).
func (s *Service) AddComment(ctx context.Context, request appmodel.TaskCommentRequest) (appmodel.TaskComment, error) {
	if request.TeamID <= 0 {
		return appmodel.TaskComment{}, appmodel.ErrInvalidTask
	}
	return s.store.AddTaskComment(ctx, request)
}

// Comments returns a task's feed in the order it was written.
// AddChecklistItem, SetChecklistItem, DeleteChecklistItem and Checklist keep a task's steps (TASK-10).
func (s *Service) AddChecklistItem(ctx context.Context, request appmodel.TaskChecklistAddRequest) (appmodel.TaskChecklistItem, error) {
	if request.TeamID <= 0 {
		return appmodel.TaskChecklistItem{}, appmodel.ErrInvalidTask
	}
	return s.store.AddChecklistItem(ctx, request)
}

func (s *Service) SetChecklistItem(ctx context.Context, request appmodel.TaskChecklistSetRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	return s.store.SetChecklistItem(ctx, request)
}

func (s *Service) DeleteChecklistItem(ctx context.Context, request appmodel.TaskChecklistDeleteRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidTask
	}
	return s.store.DeleteChecklistItem(ctx, request)
}

// Subtasks lists a task's live subtasks, for the parent's page and rollup (TASK-09).
func (s *Service) Subtasks(ctx context.Context, teamID, parentID int64) ([]model.Task, error) {
	if teamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.SubtasksOf(ctx, teamID, parentID)
}

func (s *Service) Checklist(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskChecklistItem, error) {
	if teamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.ListChecklist(ctx, teamID, taskID)
}

func (s *Service) Comments(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskComment, error) {
	if teamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.ListTaskComments(ctx, teamID, taskID)
}

// LinkedActivityTasks returns the activities that tasks hold, with each task's id and title.
func (s *Service) LinkedActivityTasks(ctx context.Context, teamID int64) (map[int64]appmodel.ActivityTask, error) {
	if teamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.LinkedActivityTasks(ctx, teamID)
}

// TrackedSecondsByTask returns the seconds each task got inside a period, for the
// task report (TASK-04).
func (s *Service) TrackedSecondsByTask(ctx context.Context, query appmodel.TaskReportQuery) (map[int64]int, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.TrackedSecondsByTask(ctx, query)
}

func (s *Service) LinkActivity(ctx context.Context, request appmodel.TaskActivityRequest) (appmodel.Activity, error) {
	if request.TeamID <= 0 {
		return appmodel.Activity{}, appmodel.ErrInvalidTask
	}
	return s.store.LinkTaskActivity(ctx, request)
}

// List returns the board for a workspace, or for one project of it.
func (s *Service) List(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.TaskRow, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	if query.Now.IsZero() {
		query.Now = s.now()
	}
	return s.store.ListTasks(ctx, query)
}

// Linkable lists the project's activities that a task can take on.
func (s *Service) Linkable(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.Activity, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidTask
	}
	return s.store.ListLinkableActivities(ctx, query)
}

// ColumnForGitHubItem maps a GitHub Projects item to a board column. A closed
// issue or pull request is done whatever its Status says. Otherwise the Status
// option name decides by its words, in English or Russian; an item with no
// Status, or a name the words do not place, starts in «к работе».
func ColumnForGitHubItem(status string, closed bool) string {
	if closed {
		return model.TaskStatusDone
	}
	name := strings.ToLower(strings.TrimSpace(status))
	switch {
	case containsAny(name, "done", "complete", "closed", "shipped", "готов", "выполн", "закрыт"):
		return model.TaskStatusDone
	case containsAny(name, "progress", "doing", "review", "active", "started", "в работе", "работ", "проверк"):
		return model.TaskStatusDoing
	default:
		return model.TaskStatusTodo
	}
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// ImportGitHubProject reads a GitHub Projects board and writes its items as
// tasks of one project. Items already imported are updated, not copied, so a
// second run is safe.
func (s *Service) ImportGitHubProject(ctx context.Context, request appmodel.GitHubImportRequest) (appmodel.TaskImportResult, error) {
	request.Token = strings.TrimSpace(request.Token)
	request.ProjectURL = strings.TrimSpace(request.ProjectURL)
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ProjectID <= 0 || request.Token == "" || request.ProjectURL == "" || len(request.Token) > 512 {
		return appmodel.TaskImportResult{}, appmodel.ErrInvalidTask
	}
	if s.source == nil {
		return appmodel.TaskImportResult{}, fmt.Errorf("github import is not configured")
	}
	board, err := s.source.FetchGitHubProject(ctx, request.Token, request.ProjectURL)
	if err != nil {
		return appmodel.TaskImportResult{}, err
	}
	items := make([]appmodel.TaskImportItem, 0, len(board))
	for _, item := range board {
		items = append(items, appmodel.TaskImportItem{
			Ref: item.Ref, Title: item.Title, URL: item.URL,
			Status: ColumnForGitHubItem(item.Status, item.Closed),
		})
	}
	return s.store.ImportTasks(ctx, appmodel.TaskImportRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, ProjectID: request.ProjectID, Items: items,
	})
}
