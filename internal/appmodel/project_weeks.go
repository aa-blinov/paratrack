package appmodel

import "time"

// ProjectWeekTotal is what the plan and the tracked time add up to on one project
// in one week (PLAN-02). Start is the first day of the week.
type ProjectWeekTotal struct {
	Start          time.Time
	PlannedMinutes int
	TrackedSeconds int
}

// ProjectWeeklyNormRequest sets a project's weekly norm in minutes; zero removes it.
type ProjectWeeklyNormRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	Minutes   int
}

// PlanMoveRequest moves what one person has planned on a project from a day onward
// to another person (TASK-03). From is the first day it applies to.
type PlanMoveRequest struct {
	TeamID     int64
	ProjectID  int64
	FromUserID int64
	ToUserID   int64
	CallerID   int64
	From       time.Time
}
