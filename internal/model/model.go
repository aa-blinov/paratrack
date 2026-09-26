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
	// EstimateMinutes is the budgeted effort for this project (nil = unset).
	EstimateMinutes *int      `json:"estimate_minutes,omitempty"`
	// BillableRateCents is the hourly rate in cents (nil = unset).
	BillableRateCents *int `json:"billable_rate_cents,omitempty"`
	// Billable marks the project as invoiceable (default true).
	Billable bool `json:"billable"`
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
	UserID             int64      `json:"user_id"`
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

// DurationSeconds returns the tracked (non-paused) seconds for a
// session. For active sessions, live time since last resume is added.
// For closed sessions this is accumulated_seconds — which UpdateSessionEnd
// now maintains — falling back to the wall-clock span for legacy rows
// that stopped before that fold existed (accumulated left at 0).
func (s Session) DurationSeconds(now time.Time) int {
	if s.EndAt != nil {
		if s.AccumulatedSeconds > 0 {
			return s.AccumulatedSeconds
		}
		// Legacy closed row: never paused, so span == tracked.
		span := int(s.EndAt.Sub(s.StartAt).Seconds())
		if span < 0 {
			span = 0
		}
		return span
	}
	total := s.AccumulatedSeconds
	if !s.Paused && s.LastResumeAt != nil {
		total += int(now.Sub(*s.LastResumeAt).Seconds())
	}
	if total < 0 {
		total = 0
	}
	return total
}

// TrackedSecondsInWindow returns the non-paused seconds attributable to
// [winStart, winEnd]. The session's tracked total is scaled by how much
// of its wall-clock span overlaps the window, so a session straddling
// midnight (or a pause) doesn't dump all of its time into one day.
func (s Session) TrackedSecondsInWindow(winStart, winEnd, now time.Time) int {
	spanStart := s.StartAt
	spanEnd := now
	if s.EndAt != nil {
		spanEnd = *s.EndAt
	}
	if !spanEnd.After(spanStart) {
		return 0
	}
	// Overlap of the wall-clock span with the window.
	ovStart, ovEnd := spanStart, spanEnd
	if ovStart.Before(winStart) {
		ovStart = winStart
	}
	if ovEnd.After(winEnd) {
		ovEnd = winEnd
	}
	if !ovEnd.After(ovStart) {
		return 0
	}
	wall := spanEnd.Sub(spanStart).Seconds()
	overlap := ovEnd.Sub(ovStart).Seconds()
	tracked := s.DurationSeconds(now)
	// Scale tracked time by the overlapping fraction of the span.
	scaled := int(float64(tracked) * overlap / wall)
	// Round sub-second overlaps up so a just-started session is visible.
	if scaled <= 0 && overlap > 0 {
		scaled = 1
	}
	return scaled
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
