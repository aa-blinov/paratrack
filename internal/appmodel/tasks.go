package appmodel

import (
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

var (
	ErrInvalidTask       = errors.New("invalid task")
	ErrTaskNotFound      = errors.New("task not found")
	ErrTaskActivityTaken = errors.New("activity already belongs to another task")
	// ErrTaskArchived refuses an edit to an archived task: it is read-only until restored.
	ErrTaskArchived = errors.New("task is archived")
)

// TaskCreateRequest creates a task in a project, with its first activity.
type TaskCreateRequest struct {
	TeamID          int64
	CallerID        int64
	ProjectID       int64
	Title           string
	Status          string
	AssigneeID      int64
	Due             string
	EstimateMinutes int
}

// TaskUpdateRequest changes the fields a form sends; a nil field is left alone.
type TaskUpdateRequest struct {
	TeamID          int64
	CallerID        int64
	TaskID          int64
	Title           *string
	AssigneeID      *int64
	Due             *string
	EstimateMinutes *int
}

// TaskStatusRequest moves a task to another board column.
type TaskStatusRequest struct {
	TeamID   int64
	CallerID int64
	TaskID   int64
	Status   string
}

// TaskArchiveRequest takes a task off the board. Its time stays.
type TaskArchiveRequest struct {
	TeamID   int64
	CallerID int64
	TaskID   int64
}

// TaskRestoreRequest puts an archived task back on the board.
type TaskRestoreRequest struct {
	TeamID   int64
	CallerID int64
	TaskID   int64
}

// TaskActivityRequest hangs an activity on a task: an existing one from the same
// project (ActivityID), or a new one with Name.
type TaskActivityRequest struct {
	TeamID     int64
	CallerID   int64
	TaskID     int64
	ActivityID int64
	Name       string
}

// TaskQuery lists a workspace's tasks: the open board by default, the archive
// when Archived is set, optionally for one project. TaskID selects a single
// task whether it is archived or not.
type TaskQuery struct {
	TeamID    int64
	ProjectID int64
	TaskID    int64
	Archived  bool
	Now       time.Time
}

// TaskRow is one task as the board and the table show it.
type TaskRow struct {
	Task         model.Task
	ProjectName  string
	ProjectColor string
	Activities   []Activity
	TrackedMin   int
}

// Activity is the public view of an activity the task holds.
type Activity struct {
	ID    int64
	Name  string
	Color string
}
