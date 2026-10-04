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
	Update    SessionUpdate
}
