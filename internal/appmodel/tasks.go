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
	// ErrTaskNameTaken refuses a task activity whose name an existing activity of the
	// team already holds, when that activity cannot be reused safely (TASK-15).
	ErrTaskNameTaken = errors.New("an activity with this name is taken")
	// ErrTaskArchived refuses an edit to an archived task: it is read-only until restored.
	ErrTaskArchived = errors.New("task is archived")
)

// TaskCreateRequest creates a task in a project, with its first activity. An
// imported task also carries the reference of the item it came from.
type TaskCreateRequest struct {
	TeamID          int64
	CallerID        int64
	ProjectID       int64
	Title           string
	Status          string
	AssigneeID      int64
	Due             string
	EstimateMinutes int
	ExternalRef     string
	ExternalURL     string
	Description     string
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
	Description     *string
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

// TaskReportQuery bounds the task report to a period [Start, End) (TASK-04). A
// session counts for the part of it inside the period; an open timer counts up to Now.
type TaskReportQuery struct {
	TeamID int64
	Start  time.Time
	End    time.Time
	Now    time.Time
}

// TaskRow is one task as the board and the table show it.
// ActivityTask is the task an activity holds: its id and its title (ADM-12, TIME-16).
type ActivityTask struct {
	TaskID int64
	Title  string
}

type TaskRow struct {
	Task         model.Task
	ProjectName  string
	ProjectColor string
	Activities   []Activity
	TrackedMin   int
}

// Errors of a GitHub Projects import. They live here so the web layer can answer
// them without importing the provider adapter.
var (
	ErrGitHubProjectURL      = errors.New("github project url is not an organization or user project")
	ErrGitHubProjectNotFound = errors.New("github project not found or not readable with this token")
	ErrGitHubTokenRejected   = errors.New("github rejected the token")
	ErrGitHubProjectTooLarge = errors.New("github project has too many items to import at once")
)

// GitHubImportRequest moves the items of a GitHub Projects board into a project's
// tasks. The token serves this one request and is never stored.
type GitHubImportRequest struct {
	TeamID     int64
	CallerID   int64
	ProjectID  int64
	ProjectURL string
	Token      string
}

// GitHubProjectItem is one item of a GitHub Projects board as the provider reads
// it. Ref is the item's stable identity: its issue or pull request URL, or the
// item id for a draft.
type GitHubProjectItem struct {
	Ref    string
	Title  string
	URL    string
	Status string
	Closed bool
}

// TaskImportItem is a board item already mapped to a board column.
type TaskImportItem struct {
	Ref    string
	Title  string
	URL    string
	Status string
}

// TaskImportRequest writes mapped board items as tasks of one project.
type TaskImportRequest struct {
	TeamID    int64
	CallerID  int64
	ProjectID int64
	Items     []TaskImportItem
}

// TaskImportResult counts what an import did to the workspace's tasks. A task
// that was archived is left alone and counted as Archived.
type TaskImportResult struct {
	Created   int
	Updated   int
	Unchanged int
	Archived  int
}

// Activity is the public view of an activity the task holds.
type Activity struct {
	ID    int64
	Name  string
	Color string
}

// TaskCommentRequest adds a line to a task's feed.
type TaskCommentRequest struct {
	TeamID   int64
	TaskID   int64
	CallerID int64
	Body     string
}

// TaskComment is one line of a task's feed: who wrote it and when.
type TaskComment struct {
	ID        int64
	TaskID    int64
	Author    string
	Body      string
	CreatedAt time.Time
}

// TaskStartRequest records that work started on a task.
type TaskStartRequest struct {
	TeamID   int64
	TaskID   int64
	CallerID int64
}
