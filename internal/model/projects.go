package model

import "time"

// ProjectSessionSpan associates a project with one session for time summaries.
type ProjectSessionSpan struct {
	ProjectID int64
	Session   Session
}

// ProjectUsage summarizes activity and tracked time for a project list row.
type ProjectUsage struct {
	ActivityCount int
	TodaySeconds  int
	MonthSeconds  int
}

// ProjectActivitySummary contains recent sessions and total time for a project.
type ProjectActivitySummary struct {
	Recent        []ActiveSession
	RecentSeconds int
	TotalSeconds  int
}

// ProjectDetail groups the team-scoped reads needed to render a project page.
type ProjectDetail struct {
	Project    Project
	Activities []Activity
	Activity   ProjectActivitySummary
	Currency   string
}

// ProjectSummary is the public subset used by session rows and exports.
type ProjectSummary struct {
	ID    int64
	Slug  string
	Name  string
	Color string
}

// Activity is a tracked activity (e.g. "reading", "work").
// TeamID is the workspace this activity belongs to. TeamID == 0 represents
// pre-workspace legacy data; application workflows require an explicit
// workspace and never use zero as an unscoped access path.
// ProjectID optionally groups an activity under a project; zero means
// "Uncategorized". Sessions inherit the project of their activity.
type Activity struct {
	ID        int64
	Name      string
	TeamID    int64
	ProjectID int64
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Project groups activities under a shared initiative, client or
// initiative ("EORA RAG", "Personal", "Side Project"). A project
// belongs to exactly one team. Color is a CSS hex string used by the
// UI to tint badges and chart slices.
type Project struct {
	ID       int64
	TeamID   int64
	Slug     string
	Name     string
	Color    string
	Archived bool
	// EstimateMinutes is the budgeted effort for this project (nil = unset).
	EstimateMinutes *int
	// BillableRateCents is the hourly rate in cents (nil = unset).
	BillableRateCents *int
	// Billable marks the project as invoiceable (default true).
	Billable  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProjectClient stores the default invoice recipient details associated with
// a project.
type ProjectClient struct {
	Name    string
	Details string
	Email   string
}
