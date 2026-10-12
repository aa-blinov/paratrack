package model

import "time"

// Client is a party an invoice is addressed to. Before this entity existed the
// recipient was a set of columns on the project (client_name, client_details,
// client_email), which made two projects for the same customer two unrelated
// records and turned every invoice into a retyping exercise.
//
// The design goal is that a CRM can grow out of this without a rewrite:
// a client is workspace-scoped, uniquely named, archived instead of deleted,
// and carries its own currency and contacts. Projects and invoices point at
// it by ID; they also keep their own snapshot of the details so that renaming
// a customer never rewrites a document that was already sent.
type Client struct {
	ID     int64
	TeamID int64
	Name   string
	// Email is the invoice delivery address. Empty means the invoice is
	// sent by hand, so the UI must not offer a "send" action for it.
	Email string
	// Details is the postal or legal address printed on the invoice.
	Details string
	// Currency overrides the workspace default for documents addressed to
	// this client ('' = inherit from the workspace, then from the project).
	Currency string
	// Notes is internal: pricing history, how they prefer to pay, who
	// signs off. Never printed on an invoice.
	Notes     string
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClientContact is one person at a client company. Kept as its own thin type
// rather than a free-form blob so that CRM-style "who do I email" lists have
// something to select from later.
type ClientContact struct {
	Name  string
	Email string
	Role  string
}
