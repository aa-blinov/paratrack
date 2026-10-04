package appmodel

// GoalUpsertRequest carries workspace authorization and target settings for a goal write.
type GoalUpsertRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	Period       string
	Minutes      int
}

// GoalDeleteRequest identifies a caller-authorized goal to remove.
type GoalDeleteRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	Period       string
}
