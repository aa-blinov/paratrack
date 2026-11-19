package appmodel

import "time"

// RecurringWorkspace is one workspace whose invoice schedule is on (UX-20).
// The shared type rides db and the invoicing workflow alike: the owner acts on
// the schedule's behalf, the zone keeps the workspace's months.
type RecurringWorkspace struct {
	TeamID  int64
	OwnerID int64
	// Zone is the workspace's own choice; a blank or broken zone answers UTC,
	// the fallback every workspace read keeps.
	Zone *time.Location
}

// RecurringLetterClaimRequest is the one "ready to issue" letter a workspace
// gets per month of the recurring invoice schedule (UX-20).
type RecurringLetterClaimRequest struct {
	TeamID int64
	// Month names the month the drafts cover (its first day, YYYY-MM-DD, the
	// same string the settings page shows).
	Month string
}
