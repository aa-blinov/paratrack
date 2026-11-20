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
// Start is the stored canonical timestamp text: the page query compares it
// byte for byte, so a re-formatted time that renders the same instant
// differently breaks the tie-break and repeats rows across pages.
type SessionCursor struct {
	Start string
	ID    int64
}

// CanonicalTimestampLayout is the one storage form of an instant: UTC, always
// nine fraction digits. Everything that writes a TEXT timestamp, or compares
// one by text (cursors, window edges), formats with this layout; a value that
// merely parses back to the same instant is not enough.
const CanonicalTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// SessionPage is one page of workspace-scoped session history.
type SessionPage struct {
	Items      []ActiveSession
	HasMore    bool
	NextCursor *SessionCursor
}

// ActiveSession pairs a session with its activity for application reads.
type ActiveSession struct {
	Session  model.Session
	Activity model.Activity
}
