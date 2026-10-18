package appmodel

import "time"

// TimesheetApprovalRequest approves or reopens one week of the timesheet. Start
// and End are the week's bounds in the approver's own location (End is the
// first instant after the week).
type TimesheetApprovalRequest struct {
	TeamID   int64
	CallerID int64
	Start    time.Time
	End      time.Time
}

// TimesheetApproval is an approved week as the timesheet shows it.
type TimesheetApproval struct {
	Start      time.Time
	End        time.Time
	ApprovedBy int64
	ApprovedAt time.Time
}
