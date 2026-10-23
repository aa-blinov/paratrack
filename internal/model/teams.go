package model

import (
	"time"
)

// TeamRole is a member's authorization level within one workspace.
type TeamRole string

const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
)

func (r TeamRole) CanManage() bool { return r == TeamRoleOwner || r == TeamRoleAdmin }

// IsOwner reports whether the role is the workspace owner's, the only one that changes
// the currency, the invoice rules, Stripe and webhooks (ADM-15).
func (r TeamRole) IsOwner() bool { return r == TeamRoleOwner }

// Team is a workspace and its owner.
type Team struct {
	ID        int64
	Slug      string
	Name      string
	OwnerID   int64
	CreatedAt time.Time
	// TimeZone and WeekStart are the workspace's own calendar (ADM-07). Blank means
	// each member keeps their own choice.
	TimeZone  string
	WeekStart string
}

// TeamMember contains a user's workspace membership and display fields.
type TeamMember struct {
	UserID   int64
	Email    string
	Name     string
	Role     TeamRole
	JoinedAt time.Time
}

// FormerMember is someone who left the workspace and can be brought back: their
// sessions, tasks and payroll history stay (ADM-04).
type FormerMember struct {
	UserID int64
	Email  string
	Name   string
	LeftAt time.Time
}

// TeamMembership is one user's relationship to a workspace, used when the
// UI needs workspace identity and role together.
type TeamMembership struct {
	Team     Team
	Role     TeamRole
	JoinedAt time.Time
}

// TeamInvite is a single-use invitation to join a workspace.
type TeamInvite struct {
	Token      string `json:"-"`
	TeamID     int64
	Role       TeamRole
	CreatedBy  int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt time.Time
	AcceptedBy int64
	// Email is the normalised address the invitation was made for, or "".
	Email string
}

func (i TeamInvite) ExpiredAt(at time.Time) bool {
	return !i.ExpiresAt.IsZero() && !at.Before(i.ExpiresAt)
}

func (i TeamInvite) Used() bool { return !i.AcceptedAt.IsZero() }
