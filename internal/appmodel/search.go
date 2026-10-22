package appmodel

import "time"

// SearchQuery is one text typed into the global search, as one viewer sees the
// workspace. Managers also find people by their email; everyone else does not (D-2).
type SearchQuery struct {
	TeamID  int64
	UserID  int64
	Manager bool
	Text    string
	Limit   int
}

// SearchHit is one match: its id, what it is called, a detail line, and for a session
// the time it started.
type SearchHit struct {
	ID     int64
	Title  string
	Detail string
	Slug   string
	At     time.Time
}

// SearchResults groups the matches by what they are.
type SearchResults struct {
	Projects []SearchHit
	Clients  []SearchHit
	Tasks    []SearchHit
	Tags     []SearchHit
	People   []SearchHit
	Sessions []SearchHit
}
