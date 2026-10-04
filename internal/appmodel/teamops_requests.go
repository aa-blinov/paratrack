package appmodel

import (
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
}

type TeamRenameRequest struct {
	TeamID   int64
	CallerID int64
	Name     string
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
	VATNote    string
}

type TeamCreateRequest struct {
	OwnerID int64
	Name    string
	Slug    string
}

type TeamInviteCreateRequest struct {
	TeamID   int64
	CallerID int64
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
