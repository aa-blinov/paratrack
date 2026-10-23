package appmodel

import "time"

// ReconcileQuery is a window to reconcile the time worked for clients against what the
// invoices bill (FIN-04). Until is exclusive; an open timer counts up to Now.
type ReconcileQuery struct {
	TeamID int64
	From   time.Time
	Until  time.Time
	Now    time.Time
}

// ClientReconciliation is one client's line of the reconciliation, in seconds:
// the time worked on the client's projects, what the invoices bill for the client,
// the worked time no invoice holds, and the difference between the worked time and
// those two (worked minus billed minus not billed).
type ClientReconciliation struct {
	ClientName       string
	WorkedSeconds    int
	BilledSeconds    int
	NotBilledSeconds int
	DiffSeconds      int
}
