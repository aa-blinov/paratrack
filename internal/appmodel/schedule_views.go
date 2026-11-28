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
	// WorkDays are the weekdays the person works, ISO digits ("12345").
	WorkDays string
}

type ScheduleViewRow struct {
	ScheduleRow
	// AvailableMinutes is capacity for the week's working days (weekdays that
	// are neither a workspace holiday nor the member's absence) less admin
	// time: the hours the week can carry for clients.
	AvailableMinutes int
	// Away names, per day of the week, the member's absence ("vacation",
	// "sick", "dayoff") or "holiday"; empty for a day to be worked.
	Away           [7]string
	LoadPercent    int
	TrackedPercent int
}

type ScheduleSnapshot struct {
	Rows         []ScheduleViewRow
	ProjectNames map[int64]string
	Projects     []model.Project
	TotalMinutes int
}
