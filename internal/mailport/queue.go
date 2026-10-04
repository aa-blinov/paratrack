package mailport

import "time"

// InvoiceEmailJob is a leased invoice message from the persistent delivery
// queue. The recipient and payload are limited to the mail delivery path.
type InvoiceEmailJob struct {
	ID        int64
	TeamID    int64
	InvoiceID int64
	Recipient string `json:"-"`
	Payload   []byte `json:"-"`
	Attempts  int
	Lease     string `json:"-"`
}

// InvoiceEmailRetryRequest releases or fails a leased message after delivery.
type InvoiceEmailRetryRequest struct {
	Job         InvoiceEmailJob
	Message     string
	AvailableAt time.Time
	MaxAttempts int
}
