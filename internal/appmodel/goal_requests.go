package appmodel

import "time"

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

// GoalTarget selects one period and target when a manager sets several goal
// periods for the same activity in one operation.
type GoalTarget struct {
	Period  string
	Minutes int
}

// GoalSetRequest applies multiple period targets atomically for one activity.
type GoalSetRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	Targets      []GoalTarget
}

// GoalUnsetRequest removes multiple period targets atomically for one activity.
type GoalUnsetRequest struct {
	TeamID       int64
	CallerID     int64
	ActivityName string
	Periods      []string
}

// GoalProgressQuery scopes goal progress reads to one team and instant.
type GoalProgressQuery struct {
	TeamID int64
	Now    time.Time
}
