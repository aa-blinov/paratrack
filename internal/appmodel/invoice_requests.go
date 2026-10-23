package appmodel

import "time"

// InvoiceLookupQuery resolves one invoice within its workspace.
type InvoiceLookupQuery struct {
	TeamID    int64
	InvoiceID int64
}

// UnbilledProjectQuery scopes billable time to one workspace and optionally a
// single project. A nil ProjectID requests all projects in the workspace.
type UnbilledProjectQuery struct {
	TeamID    int64
	ProjectID *int64
}

// InvoiceOverlapQuery finds other workspace invoices that share line labels
// within a requested period.
type InvoiceOverlapQuery struct {
	TeamID           int64
	ExcludeInvoiceID int64
	Start            time.Time
	End              time.Time
	Labels           []string
	// SessionIDs are the sessions the draft bills. An invoice that already holds an activity
	// those sessions belong to overlaps too, even when its lines were named differently (FIN-23).
	SessionIDs []int64
}

// InvoiceIndexRequest scopes the combined invoice list and draft-form reads.
type InvoiceIndexRequest struct {
	TeamID int64
}

type InvoiceMutationRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
}

type InvoiceReceiptRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	Receipt   string
}

type InvoicePaymentLinkSaveRequest struct {
	TeamID           int64
	InvoiceID        int64
	CallerID         int64
	ExpectedRevision int64
	PaymentURL       string
	StripeSessionID  string
}

type InvoiceStripePaymentRequest struct {
	TeamID          int64
	InvoiceID       int64
	StripeSessionID string
}

type InvoiceStripeLinkRequest struct {
	TeamID     int64
	InvoiceID  int64
	CallerID   int64
	SuccessURL string
}

type InvoiceManualLinkRequest struct {
	TeamID     int64
	InvoiceID  int64
	CallerID   int64
	PaymentURL string
}

type AssignBillableHistoryRequest struct {
	TeamID     int64
	ActivityID int64
	ProjectID  int64
	CallerID   int64
	Confirmed  bool
}
