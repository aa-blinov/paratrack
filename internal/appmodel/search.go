package appmodel

import "time"

// SearchQuery is one text typed into the global search, as one viewer sees the
// workspace. The database reads the viewer's role itself: managers also find clients by
// their billing email and details, and people by their email; everyone else does not (D-2).
type SearchQuery struct {
	TeamID int64
	UserID int64
	Text   string
	// Tag keeps only the clients that carry it, and their contacts.
	Tag string
	// Limit caps each kind of match; zero takes the default.
	Limit int
	// CRM says whether the contacts section is switched on for the workspace.
	CRM bool
}

// SearchHit is one match: its id, what it is called, a detail line, and for a session
// the time it started. A client carries its tags.
type SearchHit struct {
	ID     int64
	Title  string
	Detail string
	Slug   string
	At     time.Time
	Tags   []string
}

// SearchContact is a person at a client, matched by name, role, email or phone.
type SearchContact struct {
	ID         int64
	ClientID   int64
	ClientName string
	Name       string
	Role       string
	Email      string
	Phone      string
}

// SearchResults groups the matches by what they are.
type SearchResults struct {
	Projects []SearchHit
	Clients  []SearchHit
	Contacts []SearchContact
	Tasks    []SearchHit
	Tags     []SearchHit
	People   []SearchHit
	Sessions []SearchHit
}
