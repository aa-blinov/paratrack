package appmodel

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
