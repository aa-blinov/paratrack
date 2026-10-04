package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ProjectCreateRequest carries actor scope and initial project settings through
// the project workflow and its persistence transaction.
type ProjectCreateRequest struct {
	TeamID    int64
	CallerID  int64
	Name      string
	Slug      string
	Color     string
	RateCents *int
	Currency  string
}

// PayrollDraftRequest identifies the workspace, actor and period for a new
// payroll snapshot. CreatedAt is supplied by the workflow clock.
type PayrollDraftRequest struct {
	TeamID         int64
	CallerID       int64
	Notes          string
	CreatedAt      time.Time
	Start          time.Time
	End            time.Time
	ConfirmOverlap bool
}

// InvoiceDraftRequest carries the inputs for one draft invoice creation.
type InvoiceDraftRequest struct {
	TeamID   int64
	CallerID int64
	Client   string
	Notes    string
	Start    time.Time
	End      time.Time
	Options  InvoiceOptions
}

// InvoiceCreateRequest carries a prepared invoice and its lines through the
// authorized persistence transaction.
type InvoiceCreateRequest struct {
	TeamID   int64
	CallerID int64
	Number   string
	Client   string
	Start    time.Time
	End      time.Time
	Notes    string
	Lines    []model.InvoiceLine
}

// InvoiceDraftUpdateRequest carries editable invoice metadata and actor scope
// through the guarded persistence transition.
type InvoiceDraftUpdateRequest struct {
	TeamID    int64
	InvoiceID int64
	ProjectID int64
	CallerID  int64
	Client    string
	Details   string
	Email     string
	Notes     string
}

// ScheduleCellRequest describes one planned user/project cell mutation.
type ScheduleCellRequest struct {
	TeamID    int64
	ActorID   int64
	UserID    int64
	ProjectID int64
	Day       time.Time
	Minutes   int
	Note      string
}

// SavedReportCreateRequest defines an actor-owned statistics filter preset.
type SavedReportCreateRequest struct {
	TeamID      int64
	ActorID     int64
	Name        string
	Period      string
	ProjectSlug string
	Tag         string
}

// IntegrationCreateRequest carries connection credentials only through the
// authorized workflow and persistence port. Secret is sensitive.
type IntegrationCreateRequest struct {
	TeamID   int64
	CallerID int64
	Provider string
	Name     string
	Secret   string            `json:"-"`
	Config   IntegrationConfig `json:"-"`
}
