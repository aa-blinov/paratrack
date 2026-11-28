package model

import (
	"time"
)

// MemberPayrollSettings are the pay rate and planned daily capacity used by
// payroll workflows for each current workspace member. Kind picks how the
// member is paid (FIN-26): "hourly" prices their tracked time by PayCents,
// "monthly" pays MonthlyPayCents per calendar month a run covers, whatever
// the tracked time says.
type MemberPayrollSettings struct {
	UserID          int64
	Kind            string // "hourly" | "monthly"
	PayCents        int
	MonthlyPayCents int
	CapacityMinutes int
	// WorkDays are the weekdays the member works, ISO digits ("12345").
	WorkDays string
	// BillRateCents is an hour of the member for the client when the project
	// has no rate of its own; 0 when unset.
	BillRateCents int
}

// PayrollRun is an immutable snapshot of a team's payable time for a period.
type PayrollRun struct {
	ID          int64
	TeamID      int64
	Number      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Status      string // draft | paid
	Notes       string
	Currency    string
	CreatedAt   time.Time
	// PaidOn is the day the run was paid out, YYYY-MM-DD; empty while a draft
	// or for a run paid before the day was recorded.
	PaidOn string
}

// PayrollLine is one person's priced time within a payroll run.
type PayrollLine struct {
	ID          int64
	RunID       int64
	UserID      int64
	Label       string
	Seconds     int
	RateCents   int
	AmountCents int
	// Kind is PayrollLineAdjustment for a hand-added bonus or deduction and
	// empty for priced time or a salary.
	Kind string
}

// PayrollLineAdjustment marks a bonus or deduction a manager added to a draft run.
const PayrollLineAdjustment = "adjustment"

// Invoice is an issued billing document and its creation-time snapshots.
// InvoicePayment is one entry of an invoice's payment journal (FIN-08). Entries
// are only added: what the client paid is the history of entries, not a field
// that an edit could overwrite.
type InvoicePayment struct {
	ID          int64
	TeamID      int64
	InvoiceID   int64
	PaidOn      string // YYYY-MM-DD
	AmountCents int
	Note        string
	Source      string // "manual", "stripe" or "reversal"
	CreatedAt   time.Time
	// Reverses is the entry a storno reverses, 0 on an ordinary payment.
	Reverses int64
}

type Invoice struct {
	// IssuedOn is the day the invoice was sent, YYYY-MM-DD; empty for a draft.
	IssuedOn      string
	ID            int64
	TeamID        int64
	Number        string
	ClientName    string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Status        string
	Notes         string
	DueDate       string // day the payment is due (YYYY-MM-DD, "" = none), FIN-07
	PaymentURL    string
	Currency      string
	SellerDetails string
	ClientDetails string
	VATNote       string
	VATRate       int // percent; the invoice's VAT is included in its prices (FIN-16)
	ProjectID     int64
	ClientEmail   string
	Receipt       string
	ByPerson      bool
	Revision      int64
	// UnapprovedWeeks counts the weeks with unapproved time in the period when the
	// invoice was drafted; nil for an invoice made before it was recorded.
	UnapprovedWeeks *int
	CreatedAt       time.Time
}

// InvoiceLine is an invoice row. Project and session references are transient
// data used while building and persisting a draft.
type InvoiceLine struct {
	ID          int64
	InvoiceID   int64
	Label       string
	Detail      string
	Seconds     int
	RateCents   int
	AmountCents int
	Currency    string
	SessionIDs  []int64
	ProjectID   int64
}

// BillingRules configure how a team's tracked time is rounded and how new
// invoices are numbered. Logo is read with these preferences but persisted
// through a separate upload operation.
type BillingRules struct {
	RoundMinutes  int    // 0 = exact to 0.01 h; else 6, 15, 30, 60
	RoundMode     string // "up" | "nearest"
	InvoicePrefix string // "INV" → INV-2026-001
	Logo          string // data: URL of a small PNG/JPEG, "" = none
	// Recurring is the invoice schedule the workspace chose (UX-20): "" is
	// off, "1st" makes drafts for the previous month on the 1st.
	Recurring string
}

// TeamSettings groups workspace preferences needed by the settings page.
type TeamSettings struct {
	Currency   string
	Requisites string
	VATNote    string
	VATRate    int // percent, 0 for none (FIN-16)
	// HideExecutorNames keeps the members' names off by-person invoice lines (FIN-15).
	HideExecutorNames bool
	Billing           BillingRules
	// Whether Stripe credentials are stored; the secrets themselves never
	// leave the server.
	StripeKeySet    bool
	StripeSecretSet bool
}
