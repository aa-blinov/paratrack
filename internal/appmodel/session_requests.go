package appmodel

import (
	"time"
)

// SessionLookupQuery resolves one session within its workspace.
type SessionLookupQuery struct {
	TeamID    int64
	SessionID int64
}

// ClosedSessionsQuery selects closed sessions for a workspace and period,
// optionally narrowed to one activity or project.
type ClosedSessionsQuery struct {
	TeamID     int64
	Start      time.Time
	End        time.Time
	ActivityID *int64
	ProjectID  *int64
}

// SessionHistoryPageQuery selects one page of workspace session history.
type SessionHistoryPageQuery struct {
	TeamID int64
	From   time.Time
	To     time.Time
	After  *SessionCursor
	Limit  int
}

type AuthSessionCreateRequest struct {
	Token     string `json:"-"`
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

type AuthSessionDeleteRequest struct {
	Token string `json:"-"`
}

type AuthSessionsDeleteByUserRequest struct {
	UserID   int64
	CallerID int64
}

type AuthSessionTouchRequest struct {
	Token string `json:"-"`
	At    time.Time
}

type AuthSessionsPurgeExpiredRequest struct {
	Before time.Time
}

type SessionDeleteRequest struct {
	TeamID    int64
	CallerID  int64
	SessionID int64
}

type SessionUpdateRequest struct {
	TeamID    int64
	CallerID  int64
	SessionID int64
	// DurationSeconds asks persistence to derive EndAt from the locked or
	// supplied StartAt and to replace the tracked duration with this value.
	DurationSeconds *int
	// RecomputeDuration derives the tracked duration from the resulting start
	// and end values after persistence locks and loads the current session.
	RecomputeDuration bool
	Update            SessionUpdate
}
