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
