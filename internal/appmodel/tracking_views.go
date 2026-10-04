package appmodel

import (
	"github.com/aa-blinov/paratrack/internal/model"
)

// TimesheetCell is one activity row in a weekly tracked-time grid.
type TimesheetCell struct {
	ActivityID   int64
	ActivityName string
	ProjectID    int64
	Secs         [7]int
	RowTotal     int
}

// TimesheetWeek is the aggregate grid for a Monday-to-Sunday timesheet.
type TimesheetWeek struct {
	Rows       []TimesheetCell
	DayTotals  [7]int
	GrandTotal int
	Others     []model.Activity
}

// SessionCursor marks the final session returned in a descending history page.
type SessionCursor struct {
	Start string
	ID    int64
}

// SessionPage is one page of workspace-scoped session history.
type SessionPage struct {
	Items      []model.ActiveSession
	HasMore    bool
	NextCursor *SessionCursor
}
