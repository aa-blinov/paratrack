package appmodel

import "time"

// ScheduleQuery selects one workspace's plan for the requested week.
type ScheduleQuery struct {
	TeamID    int64
	WeekStart time.Time
	// Now closes the open timers, so the tracked column includes them.
	Now time.Time
}
