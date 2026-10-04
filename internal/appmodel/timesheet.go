package appmodel

import "time"

// TimesheetRequest selects one workspace week and any activity rows that
// should remain visible even when they have no entries in that week.
type TimesheetRequest struct {
	TeamID           int64
	WeekStart        time.Time
	Now              time.Time
	ExtraActivityIDs []int64
}
