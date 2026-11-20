package appmodel

import "time"

type AccountCreateRequest struct {
	Email        string
	PasswordHash string `json:"-"`
	Name         string
	TeamName     string
	// Unverified starts the account with its signup address still to be confirmed.
	Unverified bool
}

// SSOSubjectSetRequest records the provider-side subject an account was
// entered or created by through single sign-in.
type SSOSubjectSetRequest struct {
	UserID  int64
	Subject string
}

type PasswordHashUpdateRequest struct {
	UserID       int64
	CallerID     int64
	ExpectedHash string `json:"-"`
	PasswordHash string `json:"-"`
	// KeepSessionToken is the browser session that asked for the change. The
	// persistence layer revokes every other session, so a stolen session does
	// not outlive the password it was minted under. Empty revokes them all.
	KeepSessionToken string `json:"-"`
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
	UserID   int64
	CallerID int64
	Name     string
	Options  TokenOptions
}

type APITokenDeleteRequest struct {
	UserID   int64
	CallerID int64
	TokenID  int64
}

type APITokenListRequest struct {
	UserID   int64
	CallerID int64
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

// ProfileEmailRequest carries one email change. CurrentPassword is required:
// a live session alone must not be enough to move the account's login to an
// address the owner may not control.
type ProfileEmailRequest struct {
	UserID          int64
	CallerID        int64
	CurrentPassword string `json:"-"`
	Email           string
}

// ProfileEmailUpdateRequest is the persistence write behind a profile email
// change. ExpectedEmail makes the update conditional so two concurrent
// changes cannot silently overwrite each other.
type ProfileEmailUpdateRequest struct {
	UserID        int64
	CallerID      int64
	ExpectedEmail string
	Email         string
}

// PasswordChangeRequest contains credentials supplied for one password change.
type PasswordChangeRequest struct {
	UserID          int64
	CallerID        int64
	CurrentPassword string `json:"-"`
	NewPassword     string `json:"-"`
	// SessionToken is the caller's own browser session, read from the cookie by
	// the HTTP adapter. It survives the change; every other session is revoked.
	SessionToken string `json:"-"`
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
