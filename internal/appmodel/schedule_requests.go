package appmodel

import "time"

// ScheduleCopyRequest repeats the plan of the week starting at From in the
// week starting at To, seven days later. The target week must be empty.
type ScheduleCopyRequest struct {
	TeamID  int64
	ActorID int64
	From    time.Time
	To      time.Time
}

// ScheduleAdminRequest sets the admin time a member keeps each week, which
// comes off their available hours before the plan is measured against them.
type ScheduleAdminRequest struct {
	TeamID  int64
	ActorID int64
	UserID  int64
	Minutes int
}

// ScheduleQuery selects one workspace's plan for the requested week.
type ScheduleQuery struct {
	TeamID    int64
	WeekStart time.Time
	// Now closes the open timers, so the tracked column includes them.
	Now time.Time
}
