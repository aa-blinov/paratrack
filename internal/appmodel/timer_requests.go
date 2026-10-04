package appmodel

import "time"

type TimerStartRequest struct {
	TeamID     int64
	ActivityID int64
	At         time.Time
	Note       string
}

// TimerStartByNameRequest is the transport-neutral timer start action that
// resolves a workspace activity and optionally assigns it to a project first.
type TimerStartByNameRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	ProjectID    int64
	At           time.Time
	Note         string
}

type TimerFocusRequest struct {
	TeamID     int64
	ActivityID int64
	At         time.Time
}

// TimerFocusByNameRequest captures a focus action before the existing
// workspace activity is resolved.
type TimerFocusByNameRequest struct {
	TeamID       int64
	ActivityName string
	At           time.Time
}

// ImportedTaskStartRequest captures starting a timer from a workspace-scoped
// task supplied by an external integration.
type ImportedTaskStartRequest struct {
	TeamID    int64
	CallerID  int64
	TaskID    int64
	ProjectID int64
	At        time.Time
	Note      string
}

type TimerStopRequest struct {
	TeamID    int64
	SessionID int64
	At        time.Time
}

type TimerStopAllRequest struct {
	TeamID int64
	At     time.Time
}

type TimerReopenRequest struct {
	TeamID        int64
	SessionID     int64
	At            time.Time
	ExpectedEndAt time.Time
}

type TimerAddRequest struct {
	TeamID     int64
	ActivityID int64
	Start      time.Time
	End        time.Time
	Note       string
}

// TimerAddByIDRequest carries a selected activity and actor through the
// coordinated historical-session workflow.
type TimerAddByIDRequest struct {
	TeamID     int64
	CallerID   int64
	ActivityID int64
	Start      time.Time
	End        time.Time
	Note       string
}

// TimerAddByNameRequest captures backfill input before activity resolution.
type TimerAddByNameRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	ProjectID    int64
	Start        time.Time
	End          time.Time
	Note         string
}

type TimesheetCellUpdateRequest struct {
	TeamID       int64
	ActivityID   int64
	Day          time.Time
	TotalSeconds int
}

type TimerSessionRequest struct {
	TeamID    int64
	SessionID int64
	At        time.Time
}

type ActivityResolveRequest struct {
	TeamID   int64
	CallerID int64
	Name     string
}
