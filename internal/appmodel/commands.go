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
	// ClientID links the new project to a client card (0 = none yet).
	ClientID int64
	// BudgetCents is the monetary ceiling for the project (nil = unset).
	BudgetCents *int
	// Deadline is YYYY-MM-DD, "" for none. LeadUserID is a team member, 0 for none.
	Deadline   string
	LeadUserID int64
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
	// UserIDs optionally restricts the draft to a subset of members (e.g. hourly
	// contractors paid weekly vs salaried staff paid monthly). Empty means all.
	UserIDs []int64
}

// PayrollAdjustmentRequest adds a one-off bonus or deduction line to a draft pay run.
type PayrollAdjustmentRequest struct {
	TeamID   int64
	RunID    int64
	CallerID int64
	UserID   int64
	Label    string
	// AmountCents sets an explicit fixed adjustment when PercentBps == 0.
	AmountCents int
	// PercentBps computes the adjustment as basis points (100 = 1%) of the
	// member's non-adjustment earnings in the run.
	PercentBps int
}

// PayrollAdjustmentDeleteRequest removes a one-off adjustment line from a draft pay run.
type PayrollAdjustmentDeleteRequest struct {
	TeamID   int64
	RunID    int64
	LineID   int64
	CallerID int64
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
	// ConfirmUnapproved is the manager's answer to the unapproved-weeks warning
	// (FIN-12): without it a period with unapproved billable time is refused.
	ConfirmUnapproved bool
	// UnapprovedWeeks is set by the service: the weeks in the period with unapproved
	// time at the moment of the draft. Nil leaves the invoice without the record.
	UnapprovedWeeks *int
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
	DueDate   string
}

// InvoiceLineAddRequest appends a manual one-off fee or discount line to a draft invoice.
type InvoiceLineAddRequest struct {
	TeamID      int64
	InvoiceID   int64
	CallerID    int64
	Label       string
	AmountCents int
}

// InvoiceLineDeleteRequest removes a manual one-off line from a draft invoice.
type InvoiceLineDeleteRequest struct {
	TeamID    int64
	InvoiceID int64
	LineID    int64
	CallerID  int64
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
