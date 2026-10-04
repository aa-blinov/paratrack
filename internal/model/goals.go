package model

import (
	"time"
)

// Goal represents a target minutes-per-period for an activity.
type Goal struct {
	ID            int64
	ActivityID    int64
	TeamID        int64
	Period        string // daily | weekly | monthly
	TargetMinutes int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// GoalProgress combines a goal with the activity's progress in its current
// period. It is shared by the application workflow and persistence adapter.
type GoalProgress struct {
	Goal            Goal
	ActivityName    string
	AchievedMinutes int
	PercentComplete int
	PeriodStart     time.Time
	PeriodEnd       time.Time
}
