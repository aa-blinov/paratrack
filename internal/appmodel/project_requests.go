package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type AssignActivityProjectRequest struct {
	TeamID     int64
	ActivityID int64
	ProjectID  int64
	CallerID   int64
}

type ProjectMutationRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
}

type ProjectSlugMutationRequest struct {
	TeamID   int64
	Slug     string
	CallerID int64
}

type ProjectUpdateRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	Update    ProjectUpdate
}

type ProjectSlugUpdateRequest struct {
	TeamID   int64
	Slug     string
	CallerID int64
	Update   ProjectUpdate
}

type ProjectRateRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	RateCents *int
	Billable  *bool
}

// ProjectDetailRequest carries the scoped inputs for a project detail read.
type ProjectDetailRequest struct {
	TeamID          int64
	Slug            string
	IncludeArchived bool
	From            time.Time
	Through         time.Time
}

// ProjectCatalogQuery scopes a project catalog read and selects archived rows.
type ProjectCatalogQuery struct {
	TeamID          int64
	IncludeArchived bool
}

// ProjectSummariesQuery scopes a batch of project summaries to one workspace.
type ProjectSummariesQuery struct {
	TeamID     int64
	ProjectIDs []int64
}

// ProjectSlugQuery resolves a project slug within one workspace.
type ProjectSlugQuery struct {
	TeamID int64
	Slug   string
}

// ProjectScopeQuery identifies a project within its workspace.
type ProjectScopeQuery struct {
	TeamID    int64
	ProjectID int64
}

// ProjectActivityQuery scopes recent project activity to a time window.
type ProjectActivityQuery struct {
	TeamID    int64
	ProjectID int64
	From      time.Time
	Through   time.Time
}

// ProjectActivityCatalogQuery filters activities belonging to a project.
type ProjectActivityCatalogQuery struct {
	TeamID          int64
	ProjectID       int64
	IncludeArchived bool
}

// ProjectSpansQuery scopes project time spans to one workspace and range.
type ProjectSpansQuery struct {
	TeamID  int64
	From    time.Time
	Through time.Time
}

// ProjectUsageQuery combines the catalog selection with the usage window.
type ProjectUsageQuery struct {
	Catalog    ProjectCatalogQuery
	TodayStart time.Time
	MonthStart time.Time
	Now        time.Time
}

// ProjectPageRequest combines project detail with optional invoice history
// for the project page.
type ProjectPageRequest struct {
	TeamID          int64
	CallerID        int64
	Slug            string
	IncludeArchived bool
	From            time.Time
	Through         time.Time
	IncludeUnbilled bool
}

// ProjectPageSnapshot contains the workflow data needed to render project
// details without making the transport coordinate other read workflows.
type ProjectPageSnapshot struct {
	Detail        ProjectDetail
	TeamCurrency  string
	TagsBySession map[int64][]model.Tag
	ProjectsByID  map[int64]ProjectSummary
	Unbilled      []UnbilledProject
}

// SessionProjectImpactQuery asks what moving one session to a project also moves.
type SessionProjectImpactQuery struct {
	TeamID    int64
	SessionID int64
	ProjectID int64
}

// SessionProjectImpact is what a project change moves. A session with its own
// project moves alone. An activity with no project takes the project from the first
// session tagged with one, and every session of that activity without a project of
// its own follows it, unless the activity is billed. Binds says that case applies,
// and Sessions and Seconds count the sessions that would move, this one included.
type SessionProjectImpact struct {
	Binds    bool
	Sessions int
	Seconds  int
}
