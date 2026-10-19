package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

// ScheduleRow is one member's planned work during a week.
type ScheduleRow struct {
	UserID    int64
	UserName  string
	Capacity  int
	Minutes   [7]int
	Total     int
	ByProject map[int64][7]int
	// Tracked is the time the person actually logged in the week, in minutes.
	Tracked int
	// AdminMinutes is the non-client time the person keeps each week.
	AdminMinutes int
}

type ScheduleViewRow struct {
	ScheduleRow
	// AvailableMinutes is capacity for five working days less admin time: the
	// hours the week can carry for clients.
	AvailableMinutes int
	LoadPercent      int
	TrackedPercent   int
}

type ScheduleSnapshot struct {
	Rows         []ScheduleViewRow
	ProjectNames map[int64]string
	Projects     []model.Project
	TotalMinutes int
}
