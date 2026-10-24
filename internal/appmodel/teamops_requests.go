package appmodel

import (
	"slices"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type WorkspaceDeleteRequest struct {
	TeamID   int64
	CallerID int64
}

type TeamCurrencyRequest struct {
	TeamID   int64
	CallerID int64
	Currency string
}

type TeamBillingRequest struct {
	TeamID   int64
	CallerID int64
	Rules    model.BillingRules
}

type TeamStripeCredentialsRequest struct {
	TeamID   int64
	CallerID int64
	Key      string `json:"-"`
	Secret   string `json:"-"`
	// An empty Key or Secret keeps the stored one: the form never shows a
	// saved secret, so a blank field means "unchanged". Clear removes both.
	Clear bool
}

type TeamRenameRequest struct {
	TeamID   int64
	CallerID int64
	Name     string
}

// TeamCalendarRequest sets the workspace's time zone and first day of the week
// (ADM-07). Blank values give the members their own choice back. AutoLockDays is
// the age after which time closes for edits (timesheet-autolock).
type TeamCalendarRequest struct {
	TeamID       int64
	CallerID     int64
	TimeZone     string
	WeekStart    string
	AutoLockDays int
}

// AutoLockTerms are the closing ages a workspace may choose, in days. Zero means
// the past stays open.
var AutoLockTerms = []int{0, 3, 7, 14, 30}

// ValidAutoLockDays reports whether days is one of AutoLockTerms.
func ValidAutoLockDays(days int) bool {
	return slices.Contains(AutoLockTerms, days)
}

type TeamInviteRevokeRequest struct {
	TeamID   int64
	CallerID int64
	Token    string `json:"-"`
}

type TeamLogoRequest struct {
	TeamID   int64
	CallerID int64
	DataURL  string
}

type TeamModulesRequest struct {
	TeamID   int64
	CallerID int64
	Selected map[string]bool
}

type TeamRequisitesRequest struct {
	TeamID     int64
	CallerID   int64
	Requisites string
	// HideExecutorNames keeps the members' names off by-person invoice lines (FIN-15).
	HideExecutorNames bool
	// VATRate is the VAT rate in percent, 0 for none (FIN-16).
	VATRate int
	VATNote string
}

type TeamCreateRequest struct {
	OwnerID int64
	Name    string
	Slug    string
}

type TeamInviteCreateRequest struct {
	TeamID   int64
	CallerID int64
	// Email is optional. A live invitation already held for it is returned
	// instead of a second one.
	Email string
}

// TeamInvitePersistenceRequest contains the generated invitation persisted by
// the workspace workflow.
type TeamInvitePersistenceRequest struct {
	Token     string `json:"-"`
	TeamID    int64
	Role      model.TeamRole
	CallerID  int64
	CreatedAt time.Time
	ExpiresAt time.Time
	Email     string
}

type TeamInviteAcceptRequest struct {
	Token  string `json:"-"`
	UserID int64
}

// TeamInviteAcceptanceRequest is the scoped command passed to persistence
// after the invite token has resolved to its workspace.
type TeamInviteAcceptanceRequest struct {
	Token  string `json:"-"`
	TeamID int64
	UserID int64
	At     time.Time
}
