package appmodel

import "time"

// TimesheetApprovalRequest approves or reopens one week of the timesheet. Start
// and End are the week's bounds in the approver's own location (End is the
// first instant after the week). UserID 0 closes the week for the whole
// workspace; a positive id closes it for that member only (TIME-23).
type TimesheetApprovalRequest struct {
	TeamID   int64
	CallerID int64
	Start    time.Time
	End      time.Time
	UserID   int64
}

// TimesheetApproval is an approved week as the timesheet shows it.
type TimesheetApproval struct {
	Start      time.Time
	End        time.Time
	ApprovedBy int64
	ApprovedAt time.Time
	// UserID is 0 for a whole-workspace approval, or the member the week is
	// closed for.
	UserID int64
}
