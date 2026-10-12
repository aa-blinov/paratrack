package appmodel

import (
	"errors"
	"time"
)

// ErrInvalidClientTeam marks a client query that carries no workspace. A
// client id without a workspace would be an unscoped read across every
// customer in the installation, so it is refused at the service edge.
var ErrInvalidClientTeam = errors.New("client request has no workspace")

// ClientCatalogQuery scopes a client listing and selects archived rows.
type ClientCatalogQuery struct {
	TeamID          int64
	IncludeArchived bool
}

// ClientScopeQuery identifies one client inside one workspace. HTTP paths use
// the numeric ID because clients have no slug of their own — their names are
// free text, and minting a slug per client only to address it would make
// renames a URL-breaking operation.
type ClientScopeQuery struct {
	TeamID   int64
	ClientID int64
}

// ClientUsage summarises what a client is worth: projects attached, money
// billed, money still unbilled. It is read-only and derived, never stored.
type ClientUsage struct {
	ProjectCount   int
	TrackedSeconds int
	BilledCents    int
	UnbilledCents  int
}

// ProjectBudget is how far a project has run against its ceiling.
//
// SpentCents covers every finished session priced at the project rate, and
// UnbilledCents is the part of it that is not on an invoice yet. BilledCents
// is the difference. The split matters because a budget is a promise about
// total work, not about invoicing: showing only unbilled time would let a
// project look comfortably inside budget right up to the point where every
// cent of it is on paper.
//
// The arithmetic is deliberately the same one the invoice builder uses
// (rounding rules included), so the number on the project screen and the
// number on the invoice cannot drift apart.
type ProjectBudget struct {
	ProjectID int64
	// TrackedSeconds is the raw measured time behind SpentCents. Money is
	// what the ceiling is judged against; seconds are what the user actually
	// recognises as their own work.
	TrackedSeconds int64
	Currency       string
	SpentCents     int
	BilledCents    int
	UnbilledCents  int
	BudgetCents    *int
	OverBudget     bool
	// Percent is SpentCents as a share of the ceiling, rounded down, and 0
	// when no ceiling is set. Over-budget projects exceed 100 on purpose: the
	// number is a measurement, and clamping it would hide how far past the
	// line the work went.
	Percent int
	// CurrencySource names which rule produced Currency: the client card, the
	// project, or the workspace. A number without its provenance is the kind
	// of thing that gets trusted by mistake when a customer pays in dollars
	// and the screen said roubles.
	CurrencySource string
}

// ProjectBudgetQuery scopes a budget read to one workspace's projects.
type ProjectBudgetQuery struct {
	TeamID int64
}

// ClientCreateRequest mints a new client card.
type ClientCreateRequest struct {
	TeamID   int64
	CallerID int64
	Name     string
	Email    string
	Details  string
	Currency string
	Notes    string
}

// ClientUpdate is the editable set of client fields. Currency is a pointer so
// that clearing it ("inherit from the workspace") is expressible.
type ClientUpdate struct {
	Name     string
	Email    string
	Details  string
	Currency *string
	Notes    string
	Archived *bool
}

type ClientUpdateRequest struct {
	TeamID   int64
	ClientID int64
	CallerID int64
	Update   ClientUpdate
}

// ClientListItem is one row of the client directory. TrackedSeconds and the
// money columns come from the usage query so the table can answer "is this
// client worth keeping around" without a second click.
type ClientListItem struct {
	ID             int64
	Name           string
	Email          string
	Details        string
	Currency       string
	Archived       bool
	ProjectCount   int
	TrackedSeconds int
	BilledCents    int
	UnbilledCents  int
	CreatedAt      time.Time
}

// ClientProject is one project attached to a client, as shown on the
// client's own page.
type ClientProject struct {
	ID       int64
	Slug     string
	Name     string
	Color    string
	Archived bool
	// BudgetCents and SpentCents repeat the project's own figures so the
	// client page does not have to walk into every project to answer
	// "how much of this customer's work is done".
	BudgetCents  *int
	SpentCents   int
	TrackedLabel string
}

// ClientInvoice is one document addressed to this client.
type ClientInvoice struct {
	ID          int64
	Number      string
	PeriodStart string
	PeriodEnd   string
	Status      string
	Currency    string
	TotalCents  int
}
