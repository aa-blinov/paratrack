package appmodel

import "time"

// AuditListQuery scopes an audit trail to one workspace, narrows it with the
// screen filters, and bounds the result. Zero From, To, UserID, Action and
// Offset mean "no narrowing", so an address without parameters keeps showing
// the plain newest-first list.
type AuditListQuery struct {
	TeamID int64
	From   time.Time // inclusive lower bound on the event time; zero means unbounded
	To     time.Time // exclusive upper bound; zero means unbounded
	UserID int64     // actor; zero also keeps system events, which have no user
	Action string    // exact action code such as "auth.login"; empty matches every action
	Offset int       // rows the caller has already listed, so widening the window keeps them
	Limit  int
}
