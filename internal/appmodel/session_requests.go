package appmodel

import "time"

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
	// DurationSeconds asks tracking to derive EndAt from the current or
	// supplied StartAt and to replace the tracked duration with this value.
	DurationSeconds *int
	// RecomputeDuration derives the tracked duration from the resulting start
	// and end values after tracking loads the current session.
	RecomputeDuration bool
	Update            SessionUpdate
}
