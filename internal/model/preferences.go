package model

import (
	"time"
)

// Reminder triggers a notification every N minutes during a window.
type Reminder struct {
	ID           int64
	ActivityID   int64
	EveryMinutes int
	Window       *string // e.g. "09:00-18:00"
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SavedReport captures a team's reusable statistics filters.
type SavedReport struct {
	ID          int64
	TeamID      int64
	Name        string
	Period      string
	ProjectSlug string
	Tag         string
	CreatedBy   int64
	CreatedAt   time.Time
}
