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

// TimesheetRowClearRequest empties every cell of one activity inside a single
// week window. WeekStart is normalized to midnight by the store, so callers may
// pass any day of the week they are looking at.
type TimesheetRowClearRequest struct {
	TeamID     int64
	ActivityID int64
	WeekStart  time.Time
}
