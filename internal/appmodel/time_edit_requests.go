package appmodel

import "time"

// TimeEditRequest asks for a change to one timesheet cell: the minutes a person logged on one
// activity and day (TIME-19). A manager approves it, and the cell is written as the person's own.
type TimeEditRequest struct {
	ID           int64
	UserID       int64
	Person       string
	ActivityID   int64
	ActivityName string
	Day          time.Time
	Minutes      int
	Reason       string
	Status       string
	DecisionNote string
	CreatedAt    time.Time
}

// TimeEditCreateRequest is what a member sends with the request.
type TimeEditCreateRequest struct {
	TeamID     int64
	CallerID   int64
	ActivityID int64
	Day        time.Time
	Minutes    int
	Reason     string
}

// TimeEditDecisionRequest is a manager's answer to one pending request.
type TimeEditDecisionRequest struct {
	TeamID    int64
	CallerID  int64
	RequestID int64
	Approve   bool
	Note      string
}
