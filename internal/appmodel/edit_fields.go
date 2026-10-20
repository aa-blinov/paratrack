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
	// ClientID links the project to a client card; nil leaves the link
	// alone, 0 detaches it (the project keeps its recipient snapshot).
	ClientID *int64
	// BudgetCents sets the monetary ceiling; nil leaves it, and a non-nil
	// pointer to 0 clears it — an explicit 0 ceiling is meaningless, so
	// the workflow layer normalises it to nil before it reaches SQL.
	BudgetCents *int
	// Deadline sets the day the project should be done; nil leaves it, and a
	// pointer to "" clears it. LeadUserID works the same way with 0.
	Deadline   *string
	LeadUserID *int64
}

// SessionUpdate is the explicit set of editable session fields. Nil time and
// duration pointers leave those fields unchanged; an empty note clears it.
type SessionUpdate struct {
	StartAt            *time.Time
	EndAt              *time.Time
	AccumulatedSeconds *int
	// ProjectID changes the session's own project. Nil keeps it; zero returns the
	// session to its activity's project.
	ProjectID *int64
	Note      string
	UpdatedAt time.Time
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
