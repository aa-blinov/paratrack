package appmodel

// ActivityLookupQuery resolves one activity within its workspace.
type ActivityLookupQuery struct {
	TeamID     int64
	ActivityID int64
}
