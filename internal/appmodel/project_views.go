package appmodel

import (
	"github.com/aa-blinov/paratrack/internal/model"
)

// ProjectSessionSpan associates a project with one session for time summaries.
type ProjectSessionSpan struct {
	ProjectID int64
	Session   model.Session
}

// ProjectUsage summarizes activity and tracked time for a project list row.
type ProjectUsage struct {
	ActivityCount int
	TodaySeconds  int
	MonthSeconds  int
}

// ProjectActivitySummary contains recent sessions and total time for a project.
type ProjectActivitySummary struct {
	Recent        []model.ActiveSession
	RecentSeconds int
	TotalSeconds  int
}

// ProjectDetail groups the team-scoped reads needed to render a project page.
type ProjectDetail struct {
	Project         model.Project
	Activities      []model.Activity
	Activity        ProjectActivitySummary
	Currency        string
	EstimatePercent int
}

// ProjectSummary is the public subset used by session rows and exports.
type ProjectSummary struct {
	ID    int64
	Slug  string
	Name  string
	Color string
}
