package appmodel

// InvoiceEmailEnqueueRequest is the durable payload prepared by the mail workflow.
type InvoiceEmailEnqueueRequest struct {
	TeamID    int64
	InvoiceID int64
	Revision  int64
	Recipient string `json:"-"`
	Payload   []byte `json:"-"`
}
