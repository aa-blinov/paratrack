package appmodel

// FocusResult reports the timer transitions applied by a focus workflow.
type FocusResult struct {
	Paused           int
	Resumed          int
	Started          bool
	StartedSessionID int64
}

// StripeWebhookResult reports the invoice state change caused by a payment event.
type StripeWebhookResult struct {
	TeamID    int64
	InvoiceID int64
	Changed   bool
}

// PayrollPaidResult reports a payroll transition and its notification recipients.
type PayrollPaidResult struct {
	Changed    bool
	Recipients []int64
}

// ImportResult summarizes one atomically applied import batch.
type ImportResult struct {
	Imported int
	Skipped  int
}
