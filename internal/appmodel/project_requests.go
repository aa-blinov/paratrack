package appmodel

import "time"

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

type ProjectUpdateRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	Update    ProjectUpdate
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

// ProjectListQuery carries the date window used for project usage totals.
type ProjectListQuery struct {
	TeamID          int64
	IncludeArchived bool
	TodayStart      time.Time
	MonthStart      time.Time
	Now             time.Time
}
