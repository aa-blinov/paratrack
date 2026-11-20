package db

import "fmt"

// The weekly reads (team week, schedule, payroll, reconciliation) ask for
// the sessions overlapping [from, to). One predicate answered that with no
// floor on start_at: the plan walked the workspace's whole history and
// filtered row by row. Split in two, each branch keeps the floor its index
// reads: the closed sessions scan (team_id, end_at) from the window's
// start, and the open ones are the handful the partial index holds. The
// branches are disjoint, so the row set is exactly the disjunction's.
// strictEnd preserves the caller's own edge on end_at.
func windowedSessionsClosed(strictEnd bool) string {
	edge := ">="
	if strictEnd {
		edge = ">"
	}
	return fmt.Sprintf("s.team_id = ? AND s.end_at IS NOT NULL AND s.end_at %s ? AND s.start_at < ?", edge)
}

func windowedSessionsOpen() string {
	return "s.team_id = ? AND s.end_at IS NULL AND s.start_at < ?"
}
