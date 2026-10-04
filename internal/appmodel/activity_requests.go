package appmodel

// ActivityLookupQuery resolves one activity within its workspace.
type ActivityLookupQuery struct {
	TeamID     int64
	ActivityID int64
}

// ActivityNameQuery resolves an activity by name within its workspace.
type ActivityNameQuery struct {
	TeamID int64
	Name   string
}
