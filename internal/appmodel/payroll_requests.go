package appmodel

type PayrollMutationRequest struct {
	TeamID   int64
	RunID    int64
	CallerID int64
}

type PayrollMemberPayRequest struct {
	TeamID          int64
	UserID          int64
	CallerID        int64
	PayCents        *int
	CapacityMinutes *int
}
