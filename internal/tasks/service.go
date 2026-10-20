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
	CreateTask(ctx context.Context, request appmodel.TaskCreateRequest) (model.Task, error)
	UpdateTask(ctx context.Context, request appmodel.TaskUpdateRequest) (model.Task, error)
	SetTaskStatus(ctx context.Context, request appmodel.TaskStatusRequest) error
	ArchiveTask(ctx context.Context, request appmodel.TaskArchiveRequest) error
	RestoreTask(ctx context.Context, request appmodel.TaskRestoreRequest) error
	LinkTaskActivity(ctx context.Context, request appmodel.TaskActivityRequest) (appmodel.Activity, error)
	ListTasks(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.TaskRow, error)
	ListLinkableActivities(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.Activity, error)
}

// Service validates what a person typed, then hands it to the store.
type Service struct {
	store Store
	now   func() time.Time
}

// New builds the task service. Now defaults to the wall clock.
func New(store Store, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("tasks store is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}, nil
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
