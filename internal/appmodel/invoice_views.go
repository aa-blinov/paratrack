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
	Invoice model.Invoice
	Lines   []model.InvoiceLine
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
