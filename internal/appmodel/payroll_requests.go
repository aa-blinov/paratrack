package appmodel

// PayrollRunLookupQuery resolves one payroll run within its workspace.
type PayrollRunLookupQuery struct {
	TeamID int64
	RunID  int64
}

// PayrollRunListQuery scopes a payroll run catalog to one workspace.
type PayrollRunListQuery struct {
	TeamID int64
}

type PayrollMutationRequest struct {
	TeamID   int64
	RunID    int64
	CallerID int64
	// PaidOn is the payout day for marking a run paid, YYYY-MM-DD; empty
	// means today in the workspace zone.
	PaidOn string
}

type PayrollMemberPayRequest struct {
	TeamID          int64
	UserID          int64
	CallerID        int64
	PayCents        *int
	CapacityMinutes *int
	// Kind switches how the member is paid (FIN-26): nil keeps the current
	// kind, "hourly" prices tracked time by PayCents, "monthly" pays the
	// fixed MonthlyPayCents per calendar month a run covers.
	Kind            *string
	MonthlyPayCents *int
}
