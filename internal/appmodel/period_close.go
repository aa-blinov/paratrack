package appmodel

import "time"

// PeriodCloseRequest closes or reopens one month of the workspace (FIN-25).
// Month is the month's first day as "2006-01-02". It is a calendar name, not
// a moment: a day filled in by a viewer in one zone must not slide into the
// neighbouring month in the workspace zone, so the store normalises to the
// string and never re-derives the month from an instant.
type PeriodCloseRequest struct {
	TeamID   int64
	CallerID int64
	Month    string
}

// PeriodClose is one closed month as the workflow hands it to a view: Month is
// the month's first day in the workspace zone, ClosedOn is the close's instant.
type PeriodClose struct {
	Month      time.Time
	ClosedOn   time.Time
	ClosedByID int64
}
