package appmodel

import "time"

// ProjectUpdate contains optional fields accepted by a project edit.
// Nil pointers leave fields unchanged; an estimate of zero clears the estimate.
type ProjectUpdate struct {
	Name            string
	Color           string
	Archived        *bool
	EstimateMinutes *int
	RateCents       *int
	Billable        *bool
	Currency        *string
}

// SessionUpdate is the explicit set of editable session fields. Nil time and
// duration pointers leave those fields unchanged; an empty note clears it.
type SessionUpdate struct {
	StartAt            *time.Time
	EndAt              *time.Time
	AccumulatedSeconds *int
	Note               string
	UpdatedAt          time.Time
}

// InvoiceOptions are creation-time metadata saved with an invoice.
type InvoiceOptions struct {
	ClientDetails  string
	ClientEmail    string
	ProjectID      int64
	ByPerson       bool
	RememberClient bool
}

// TokenOptions define the scope and lifetime of a newly minted API token.
type TokenOptions struct {
	TeamID    int64
	ExpiresAt *time.Time
	ReadOnly  bool
}
