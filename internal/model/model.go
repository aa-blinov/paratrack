// Package model contains shared domain types for paratrack.
package model

import "time"

// Activity is a tracked activity (e.g. "reading", "work").
// TeamID is the workspace this activity belongs to; rows with
// TeamID == 0 are pre-team legacy data visible only when no
// scope is requested (the auth middleware always supplies a real id).
// ProjectID optionally groups an activity under a project; zero means
// "Uncategorized". Sessions inherit the project of their activity.
type Activity struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	TeamID    int64     `json:"team_id"`
	ProjectID int64     `json:"project_id"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Project groups activities under a shared initiative, client or
// initiative ("EORA RAG", "Personal", "Side Project"). A project
// belongs to exactly one team. Color is a CSS hex string used by the
// UI to tint badges and chart slices.
type Project struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session is a single time-tracking interval for an activity.
// Sessions may be active (end_at == nil), paused (paused == true),
// or finished (end_at != nil). Active duration is computed as:
//
//	accumulated_seconds + (paused ? 0 : now - last_resume_at)
type Session struct {
	ID                 int64      `json:"id"`
	ActivityID         int64      `json:"activity_id"`
	TeamID             int64      `json:"team_id"`
	StartAt            time.Time  `json:"start_at"`
	EndAt              *time.Time `json:"end_at,omitempty"`
	Note               *string    `json:"note,omitempty"`
	Paused             bool       `json:"paused"`
	PausedAt           *time.Time `json:"paused_at,omitempty"`
	AccumulatedSeconds int        `json:"accumulated_seconds"`
	LastResumeAt       *time.Time `json:"last_resume_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// DurationSeconds returns the wall-clock seconds a session has accumulated.
// For active sessions, live time since last resume is added.
func (s Session) DurationSeconds(now time.Time) int {
	total := s.AccumulatedSeconds
	if !s.Paused && s.EndAt == nil && s.LastResumeAt != nil {
		total += int(now.Sub(*s.LastResumeAt).Seconds())
	}
	if total < 0 {
		total = 0
	}
	return total
}

// Active reports whether the session is in progress (open and not finished).
func (s Session) Active() bool {
	return s.EndAt == nil
}

// Tag is a free-form label attachable to sessions.
type Tag struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	TeamID    int64      `json:"team_id"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionTag links a session to a tag (many-to-many).
type SessionTag struct {
	SessionID int64 `json:"session_id"`
	TagID     int64 `json:"tag_id"`
}

// Goal represents a target minutes-per-period for an activity.
type Goal struct {
	ID            int64     `json:"id"`
	ActivityID    int64     `json:"activity_id"`
	TeamID        int64      `json:"team_id"`
	Period        string    `json:"period"` // daily | weekly | monthly
	TargetMinutes int       `json:"target_minutes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Reminder triggers a notification every N minutes during a window.
type Reminder struct {
	ID           int64     `json:"id"`
	ActivityID   int64     `json:"activity_id"`
	EveryMinutes int       `json:"every_minutes"`
	Window       *string   `json:"window,omitempty"` // e.g. "09:00-18:00"
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ActiveSession pairs a session with its activity for display.
type ActiveSession struct {
	Session  Session
	Activity Activity
}
