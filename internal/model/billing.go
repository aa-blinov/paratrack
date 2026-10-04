package model

import (
	"time"
)

// MemberPayrollSettings are the pay rate and planned daily capacity used by
// payroll workflows for each current workspace member.
type MemberPayrollSettings struct {
	UserID          int64
	PayCents        int
	CapacityMinutes int
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
}

// PayrollRunDetails is the read model used to render a run with its immutable
// line snapshot.
type PayrollRunDetails struct {
	Run   PayrollRun
	Lines []PayrollLine
}

// Invoice is an issued billing document and its creation-time snapshots.
type Invoice struct {
	ID            int64
	TeamID        int64
	Number        string
	ClientName    string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Status        string
	Notes         string
	PaymentURL    string
	Currency      string
	SellerDetails string
	ClientDetails string
	VATNote       string
	ProjectID     int64
	ClientEmail   string
	Receipt       string
	ByPerson      bool
	Revision      int64
	CreatedAt     time.Time
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

// InvoiceDetails is the read model used to render an invoice with its frozen
// line snapshot.
type InvoiceDetails struct {
	Invoice Invoice
	Lines   []InvoiceLine
}

// UnbilledProject is billable project time not yet assigned to an invoice.
type UnbilledProject struct {
	ProjectID   int64
	ProjectName string
	ProjectSlug string
	Currency    string
	Hundredths  int
	AmountCents int
	Since       time.Time
}

// UnassignedActivity has completed, not-yet-billed history without a project.
type UnassignedActivity struct {
	ID, Sessions int64
	Name         string
	Billed       bool
}

// BillingRules configure how a team's tracked time is rounded and how new
// invoices are numbered. Logo is read with these preferences but persisted
// through a separate upload operation.
type BillingRules struct {
	RoundMinutes  int    // 0 = exact to 0.01 h; else 6, 15, 30, 60
	RoundMode     string // "up" | "nearest"
	InvoicePrefix string // "INV" → INV-2026-001
	Logo          string // data: URL of a small PNG/JPEG, "" = none
}

// TeamSettings groups workspace preferences needed by the settings page.
type TeamSettings struct {
	Currency   string
	Requisites string
	VATNote    string
	Billing    BillingRules
}
