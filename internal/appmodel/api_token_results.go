package appmodel

import "time"

// APITokenIdentity is the authentication policy projection used to build a
// request principal. It exposes no persistence metadata unrelated to auth.
type APITokenIdentity struct {
	UserID   int64
	TeamID   int64
	ReadOnly bool
}

// APITokenSummary contains the non-secret fields shown in token management.
type APITokenSummary struct {
	ID         int64
	UserID     int64
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	TeamID     int64
	ExpiresAt  *time.Time
	ReadOnly   bool
}

type APITokenManagementSnapshot struct {
	Tokens    []APITokenSummary
	TeamNames map[int64]string
}
