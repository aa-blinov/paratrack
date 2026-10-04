package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

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
	Unbilled     []model.UnbilledProject
	Unassigned   []model.UnassignedActivity
}

// WebhookManagementSnapshot contains the credential-free data needed by the
// webhook settings page.
