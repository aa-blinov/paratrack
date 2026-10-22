package appmodel

import "time"

// PersonDayQuery asks what each member tracked on each day of a stretch of days
// (PLAN-04). Days are the starts of consecutive days in the viewer's zone; an open
// timer counts up to Now.
type PersonDayQuery struct {
	TeamID int64
	Days   []time.Time
	Now    time.Time
}

// DayMarkRequest records or clears a member's own mark on one day: an overload or an
// idle day, with a note. An empty kind clears the day's mark (TIME-13).
type DayMarkRequest struct {
	TeamID int64
	UserID int64
	Day    string
	Kind   string
	Note   string
	Now    time.Time
}

// DayMark is one member's mark on a day (TIME-13).
type DayMark struct {
	UserID int64
	Day    string
	Kind   string
	Note   string
}
