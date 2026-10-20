package model

import "time"

// Task statuses, in board order. A task is the work; the activities it holds
// are the time buckets the timer writes to.
const (
	TaskStatusTodo  = "todo"
	TaskStatusDoing = "doing"
	TaskStatusDone  = "done"
)

// Task is one piece of work in a project. It is archived, never deleted, so the
// time its activities hold stays attached to it.
type Task struct {
	ID              int64
	TeamID          int64
	ProjectID       int64
	Title           string
	Status          string
	Position        int
	AssigneeID      int64
	Due             string // YYYY-MM-DD, "" for none
	EstimateMinutes int
	CreatedBy       int64
	Archived        bool
	ExternalURL     string // the GitHub item this task was imported from, "" if none
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
