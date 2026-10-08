package model

import "time"

// Activity is a tracked activity (e.g. "reading", "work").
// TeamID is the workspace this activity belongs to. TeamID == 0 represents
// pre-workspace legacy data; application workflows require an explicit
// workspace and never use zero as an unscoped access path.
// ProjectID optionally groups an activity under a project; zero means
// "Uncategorized". Sessions inherit the project of their activity.
type Activity struct {
	ID   int64
	Name string
	// Color is the activity's own mark, assigned once and stored — not hashed
	// from the name, which made different activities share a colour.
	Color     string
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
