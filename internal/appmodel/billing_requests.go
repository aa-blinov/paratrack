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

type ManualPaymentRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	ClientIP  string
}
