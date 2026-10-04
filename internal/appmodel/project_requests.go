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

// ProjectListQuery carries the date window used for project usage totals.
type ProjectListQuery struct {
	TeamID          int64
	IncludeArchived bool
	TodayStart      time.Time
	MonthStart      time.Time
	Now             time.Time
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
