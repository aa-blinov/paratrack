package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type DashboardQuery struct {
	TeamID         int64
	Now            time.Time
	IncludeBilling bool
}

type DashboardSnapshot struct {
	Activities        []model.Activity
	ActiveSessions    []ActiveSession
	TodaySessions     []ActiveSession
	RecentSessions    []ActiveSession
	Projects          []model.Project
	TagsBySession     map[int64][]model.Tag
	ProjectsByID      map[int64]ProjectSummary
	Goals             []GoalProgress
	Unbilled          []UnbilledProject
	TodayStart        time.Time
	TodayEnd          time.Time
	RecentStart       time.Time
	RecentEnd         time.Time
	TodayTotalSeconds int
	TopActivityName   string
	HasSession        bool
}

// ActiveListSnapshot combines the tracking and workspace data needed to
// render the active-session list without exposing presentation types.
type ActiveListSnapshot struct {
	ActiveSessions []ActiveSession
	Projects       []model.Project
	TagsBySession  map[int64][]model.Tag
	ProjectsByID   map[int64]ProjectSummary
	FirstRun       bool
}

// SessionDecorationRequest selects the row metadata that a consumer needs
// for a batch of sessions.
type SessionDecorationRequest struct {
	TeamID          int64
	Sessions        []ActiveSession
	IncludeTags     bool
	IncludeProjects bool
}

// SessionDecorationSnapshot contains optional tag and project metadata keyed
// by session or project ID.
type SessionDecorationSnapshot struct {
	TagsBySession map[int64][]model.Tag
	ProjectsByID  map[int64]ProjectSummary
}

type SessionDecorationRowSnapshot struct {
	Session     ActiveSession
	Decorations SessionDecorationSnapshot
}

// ProjectListSnapshot combines projects with the workflow-calculated usage
// values displayed beside them.
//
// ClientNames and ActivitiesByProject are what a table-shaped list needs and
// a card could not fit: the client a project is billed to, and the activity
// names behind the count. Both are one workspace-wide read each, not a read
// per project.
type ProjectListSnapshot struct {
	Projects            []model.Project
	Usage               map[int64]ProjectUsage
	ClientNames         map[int64]string
	ActivitiesByProject map[int64][]ProjectActivityRef
}

// ProjectActivityRef is one activity name and colour inside the project list.
type ProjectActivityRef struct {
	Name   string
	Color  string
	Hidden bool // archived activity, still linked to its sessions
}

// ProjectCatalogSnapshot combines projects with their activity counts for
// adapters that do not need time-window usage totals.
type ProjectCatalogSnapshot struct {
	Projects       []model.Project
	ActivityCounts map[int64]int
}

// GoalManagementSnapshot combines the activity catalog and current progress
// used by the goal management page.
type GoalManagementSnapshot struct {
	Activities []model.Activity
	Progress   []GoalProgress
}

// TimerStopResult carries the stopped session and its duration as calculated
// at the stop request's timestamp.
type TimerStopResult struct {
	Session         model.Session
	DurationSeconds int
	ActivityName    string
}

// InvoiceDraftOptions combines workspace projects with their saved client
