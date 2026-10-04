package model

import (
	"time"
)

// User is an account record shared by the authentication service and storage.
type User struct {
	ID           int64
	Email        string
	PasswordHash string `json:"-"`
	Name         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AuthSession is a persisted browser login session.
type AuthSession struct {
	Token      string `json:"-"`
	UserID     int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// APIToken is a named bearer credential. The raw token is never stored or
// included in the persisted record.
type APIToken struct {
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
