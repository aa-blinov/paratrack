package model

import (
	"strings"
	"time"
)

// TeamRole is a member's authorization level within one workspace.
type TeamRole string

const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
	// TeamRoleAccountant tracks their own time like a member and, besides,
	// reads the money: invoices, payouts and reports, their exports, records
	// the payments that came in and closes a month. They never edit time,
	// rates, documents or settings.
	TeamRoleAccountant TeamRole = "accountant"
)

func (r TeamRole) CanManage() bool { return r == TeamRoleOwner || r == TeamRoleAdmin }

// CanReadFinance reports whether the role sees the invoices, the payouts and
// the reports, and may record a payment and close a month.
func (r TeamRole) CanReadFinance() bool { return r.CanManage() || r == TeamRoleAccountant }

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
	// AutoLockDays closes time that starts more than this many days ago for
	// edits. Zero leaves the past open, as it was before the setting existed.
	AutoLockDays int
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

// DefaultWorkDays are Monday to Friday, as ISO weekday digits.
const DefaultWorkDays = "12345"

// WorksOn says whether a member with these work days works on a weekday. An
// empty set reads as the default week.
func WorksOn(workDays string, day time.Weekday) bool {
	if workDays == "" {
		workDays = DefaultWorkDays
	}
	iso := int(day)
	if iso == 0 {
		iso = 7
	}
	return strings.ContainsRune(workDays, rune('0'+iso))
}

// NormalizeWorkDays keeps the ISO weekday digits of a set once each, in
// order, and answers false when nothing is left.
func NormalizeWorkDays(raw string) (string, bool) {
	var out []byte
	for d := byte('1'); d <= '7'; d++ {
		if strings.IndexByte(raw, d) >= 0 {
			out = append(out, d)
		}
	}
	return string(out), len(out) > 0
}
