package appmodel

import "time"

type StripePaymentEvent struct {
	Signature  string `json:"-"`
	Body       []byte `json:"-"`
	ReceivedAt time.Time
}

type StripeWebhookRequest struct {
	Event    StripePaymentEvent
	ClientIP string
}

// InvoicePaymentRequest books one payment into an invoice's journal. PaidOn is
// the day the money arrived, as YYYY-MM-DD.
type InvoicePaymentRequest struct {
	TeamID      int64
	InvoiceID   int64
	CallerID    int64
	PaidOn      string
	AmountCents int
	Note        string
}

type ManualPaymentRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	ClientIP  string
}
