package appmodel

import "time"

type AccountCreateRequest struct {
	Email        string
	PasswordHash string `json:"-"`
	Name         string
	TeamName     string
}

type PasswordHashUpdateRequest struct {
	UserID       int64
	CallerID     int64
	ExpectedHash string `json:"-"`
	PasswordHash string `json:"-"`
}

type PasswordResetCreateRequest struct {
	TokenHash string `json:"-"`
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

type PasswordResetConsumeRequest struct {
	TokenHash    string `json:"-"`
	LegacyToken  string `json:"-"`
	PasswordHash string `json:"-"`
	At           time.Time
}

type APITokenCreateRequest struct {
	UserID  int64
	Name    string
	Options TokenOptions
}

type APITokenDeleteRequest struct {
	UserID  int64
	TokenID int64
}

// APITokenLookupRequest carries a bearer secret to the token lookup that also
// records last use. Raw must never be logged or included in audit metadata.
type APITokenLookupRequest struct {
	Raw string `json:"-"`
}

type ProfileNameRequest struct {
	UserID   int64
	CallerID int64
	Name     string
}

// PasswordChangeRequest contains credentials supplied for one password change.
type PasswordChangeRequest struct {
	UserID          int64
	CallerID        int64
	CurrentPassword string `json:"-"`
	NewPassword     string `json:"-"`
}

type PasswordLoginRequest struct {
	Email    string
	Password string `json:"-"`
}

type PasswordResetRequest struct {
	Email string
}

type PasswordResetCompletionRequest struct {
	Token       string `json:"-"`
	NewPassword string `json:"-"`
}

type LogoutRequest struct {
	TeamID int64
	UserID int64
	Token  string `json:"-"`
}
