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

// ActivityLookupQuery resolves one activity within its workspace.
type ActivityLookupQuery struct {
	TeamID     int64
	ActivityID int64
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
	Detail        model.ProjectDetail
	TeamCurrency  string
	TagsBySession map[int64][]model.Tag
	ProjectsByID  map[int64]model.ProjectSummary
	Unbilled      []model.UnbilledProject
}
