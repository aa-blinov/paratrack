package appmodel

import "time"

// WorkdayRequest marks the start or the end of a member's working day (TIME-06). The
// start is kept once; the end is the last one the member gave.
type WorkdayRequest struct {
	TeamID int64
	UserID int64
	Day    string
	Action string
	Now    time.Time
}

// WorkdayMark is a member's day in the workspace: when it started and, once given,
// when it ended. A zero time means the mark has not been set.
type WorkdayMark struct {
	Day       string
	StartedAt time.Time
	EndedAt   time.Time
}
