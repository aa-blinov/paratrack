package appmodel

// ReminderEmailEnqueueRequest is a reminder letter that is not tied to an invoice.
// The recipient and payload stay out of logs and JSON, like the invoice letter's.
type ReminderEmailEnqueueRequest struct {
	TeamID    int64
	Recipient string `json:"-"`
	Payload   []byte `json:"-"`
}

// InvoiceEmailEnqueueRequest is the durable payload prepared by the mail workflow.
type InvoiceEmailEnqueueRequest struct {
	TeamID    int64
	InvoiceID int64
	Revision  int64
	Recipient string `json:"-"`
	Payload   []byte `json:"-"`
}
