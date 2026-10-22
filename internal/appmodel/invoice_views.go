package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type InvoiceDraftOptions struct {
	Projects    []InvoiceProjectOption
	HasBillable bool
}

type InvoiceProjectOption struct {
	Project   model.Project
	Client    model.ProjectClient
	HasClient bool
	Eligible  bool
}

// InvoiceDetails joins an invoice with its immutable line snapshot for reads.
type InvoiceDetails struct {
	Invoice  model.Invoice
	Lines    []model.InvoiceLine
	Payments []model.InvoicePayment
}

// UnbilledProject is billable project time not yet assigned to an invoice.
type UnbilledProject struct {
	ProjectID   int64
	ProjectName string
	ProjectSlug string
	Currency    string
	Hundredths  int
	AmountCents int
	Since       time.Time
}

// UnassignedActivity has completed, not-yet-billed history without a project.
type UnassignedActivity struct {
	ID, Sessions int64
	Name         string
	Billed       bool
}

type InvoiceStripePaymentLink struct {
	URL           string
	InvoiceNumber string
}

type InvoiceDraftCreation struct {
	Invoice       model.Invoice
	Lines         []model.InvoiceLine
	Overlaps      []string
	AdvisoryError error
}

// InvoiceDetailResult contains the invoice snapshot and workflow-calculated totals.
type InvoiceDetailResult struct {
	Invoice              model.Invoice
	Lines                []model.InvoiceLine
	TotalCents           int
	TotalHoursHundredths int
	// InvoicedSeconds is the time the lines carry. TrackedSeconds is the time the
	// invoice's sessions carry now; it is read for drafts only, the statuses whose
	// sessions can still change.
	InvoicedSeconds int
	TrackedSeconds  int
	// PaidCents is what the payment journal holds; Payments is the journal itself.
	PaidCents int
	Payments  []model.InvoicePayment
}

// InvoiceDocumentRequest scopes the shared invoice detail read used by the
// invoice page, printable documents, and outbound email.
type InvoiceDocumentRequest struct {
	TeamID                 int64
	InvoiceID              int64
	IncludeStripeReadiness bool
}

// InvoiceDocumentSnapshot combines invoice details with the workspace billing
// rules needed to render an invoice or act.
type InvoiceDocumentSnapshot struct {
	Details      InvoiceDetailResult
	BillingRules model.BillingRules
	StripeReady  bool
}

type InvoiceSummaryResult struct {
	Invoice              model.Invoice
	TotalCents           int
	TotalHoursHundredths int
	PaidCents            int
}

// InvoiceIndexSnapshot contains the workflow data needed by the invoice list
// and draft form.
type InvoiceIndexSnapshot struct {
	Invoices     []InvoiceSummaryResult
	DraftOptions InvoiceDraftOptions
	Unbilled     []UnbilledProject
	Unassigned   []UnassignedActivity
}

// WebhookManagementSnapshot contains the credential-free data needed by the
// webhook settings page.

// InvoiceSessionRow is one tracked session billed on an invoice, as the invoice's
// appendix lists it: who, where, when and how long (FIN-06).
type InvoiceSessionRow struct {
	Person   string
	Project  string
	Activity string
	Start    time.Time
	End      time.Time
	Seconds  int
}

// InvoiceExportQuery selects the invoice lines an accountant downloads: the issued
// invoices of one workspace, optionally for one client and a period of invoice dates
// (FIN-05). A zero From or To leaves that side open; To is exclusive.
type InvoiceExportQuery struct {
	TeamID int64
	Client string
	From   time.Time
	To     time.Time
}

// InvoiceExportLine is one line of an issued invoice, as the accountant's export lists it.
type InvoiceExportLine struct {
	InvoiceNumber string
	IssuedAt      time.Time
	Client        string
	Status        string
	Label         string
	Seconds       int
	RateCents     int
	AmountCents   int
	Currency      string
}
