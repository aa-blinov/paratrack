package appmodel

import "time"

// InvoiceShareRequest makes a client's link to one invoice (FIN-13). Only the hash of
// the token reaches the store; the token is handed to the manager once.
type InvoiceShareRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	TokenHash string
	Now       time.Time
	ExpiresAt time.Time
}

// InvoiceShareRevokeRequest closes every client link to one invoice.
// InvoiceVoidRequest voids a sent invoice that nothing was paid on.
type InvoiceVoidRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	Reason    string
}

type InvoiceShareRevokeRequest struct {
	TeamID    int64
	InvoiceID int64
	CallerID  int64
	// LinkID closes one link; 0 closes every link of the invoice.
	LinkID int64
}

// InvoiceShareLinkView is one client link that still opens.
type InvoiceShareLinkView struct {
	ID        int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// InvoiceShareLookupQuery opens a client's link by the hash of its token, while the
// link has not expired.
type InvoiceShareLookupQuery struct {
	TokenHash string
	Now       time.Time
	// TeamID and InvoiceID list an invoice's links (TokenHash empty).
	TeamID, InvoiceID int64
}

// InvoiceShareLookup is what a client's link opens: the workspace and the invoice.
type InvoiceShareLookup struct {
	TeamID    int64
	InvoiceID int64
}
