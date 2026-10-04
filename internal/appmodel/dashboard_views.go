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
	ActiveSessions    []model.ActiveSession
	TodaySessions     []model.ActiveSession
	RecentSessions    []model.ActiveSession
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
	ActiveSessions []model.ActiveSession
	Projects       []model.Project
	TagsBySession  map[int64][]model.Tag
	ProjectsByID   map[int64]ProjectSummary
	FirstRun       bool
}

// SessionDecorationRequest selects the row metadata that a consumer needs
// for a batch of sessions.
type SessionDecorationRequest struct {
	TeamID          int64
	Sessions        []model.ActiveSession
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
	Session     model.ActiveSession
	Decorations SessionDecorationSnapshot
}

// ProjectListSnapshot combines projects with the workflow-calculated usage
// values displayed beside them.
type ProjectListSnapshot struct {
	Projects []model.Project
	Usage    map[int64]ProjectUsage
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
