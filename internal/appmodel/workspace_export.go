package appmodel

import (
	"errors"
	"time"
)

// WorkspaceExportFormat names the export file. WorkspaceExportVersion changes
// when a field changes meaning; a reader refuses any other version.
const (
	WorkspaceExportFormat  = "paratrack-workspace"
	WorkspaceExportVersion = 1
)

// WorkspaceExportRequest asks for one workspace export on behalf of a caller.
type WorkspaceExportRequest struct {
	TeamID   int64
	CallerID int64
}

// WorkspaceExport is what a workspace holds that a person needs to move it:
// the people (by email), the work, the money and the plans. Secrets are not in
// it: no password hashes, sign-in or API tokens, payment keys, webhook secrets
// or integration secrets. Times stay as the database stores them, so a restore
// writes back exactly what was exported.
type WorkspaceExport struct {
	Format             string
	Version            int
	ExportedAt         time.Time
	Team               ExportTeam
	Members            []ExportMember
	Clients            []ExportClient
	Projects           []ExportProject
	Activities         []ExportActivity
	Tags               []ExportTag
	Sessions           []ExportSession
	Goals              []ExportGoal
	Tasks              []ExportTask
	Invoices           []ExportInvoice
	PayrollRuns        []ExportPayrollRun
	ScheduleEntries    []ExportSchedule
	ScheduleLines      []ExportScheduleRow
	TimesheetApprovals []ExportApproval
	SavedReports       []ExportReport
	Webhooks           []ExportWebhook
	Integrations       []ExportIntegration
}

// ExportTeam holds the workspace settings a new workspace starts from.
type ExportTeam struct {
	Slug          string
	Name          string
	Currency      string
	RoundMinutes  int
	RoundMode     string
	InvoicePrefix string
	Requisites    string
	VATNote       string
	Modules       string
	Logo          string
}

// ExportMember is a person in the workspace. Email is the identity every other
// record refers to, so a restore can find the same person.
type ExportMember struct {
	Email           string
	Name            string
	Role            string
	HourlyPayCents  int64
	CapacityMinutes int64
	AdminMinutes    int64
	JoinedAt        string
}

type ExportClient struct {
	ID        int64
	Name      string
	Email     string
	Details   string
	Currency  string
	Notes     string
	Archived  bool
	CreatedAt string
	UpdatedAt string
}

type ExportProject struct {
	ID                int64
	Slug              string
	Name              string
	Color             string
	Archived          bool
	ClientID          int64
	ClientName        string
	ClientDetails     string
	ClientEmail       string
	Billable          bool
	BillableRateCents int64
	Currency          string
	BudgetCents       int64
	EstimateMinutes   int64
	Deadline          string
	LeadEmail         string
	CreatedAt         string
	UpdatedAt         string
}

type ExportActivity struct {
	ID        int64
	Name      string
	ProjectID int64
	Archived  bool
	Color     string
	TaskID    int64
}

type ExportTag struct {
	ID   int64
	Name string
}

// ExportSession is one finished stretch of tracked time. Its tags and its
// invoice are referred to by the ids the export gives them. Running and paused
// timers are not exported.
type ExportSession struct {
	ID                 int64
	UserEmail          string
	ActivityID         int64
	StartAt            string
	EndAt              *string
	Note               string
	Paused             bool
	PausedAt           *string
	AccumulatedSeconds int64
	LastResumeAt       *string
	TagIDs             []int64
	InvoiceID          int64
	ExternalID         string
	CreatedAt          string
	UpdatedAt          string
}

type ExportGoal struct {
	ID            int64
	ActivityID    int64
	Period        string
	TargetMinutes int64
}

type ExportTask struct {
	ID              int64
	ProjectID       int64
	Title           string
	Status          string
	Position        int
	AssigneeEmail   string
	DueDate         string
	EstimateMinutes int
	CreatedByEmail  string
	Archived        bool
	ExternalRef     string
	ExternalURL     string
	CreatedAt       string
	UpdatedAt       string
}

// ExportInvoice keeps the billed numbers and text. Payment-provider links and
// session ids are left out: the first is the provider's, the second is local.
type ExportInvoice struct {
	ID            int64
	Number        string
	ClientName    string
	ClientEmail   string
	ClientDetails string
	ProjectID     int64
	PeriodStart   string
	PeriodEnd     string
	Status        string
	Notes         string
	Receipt       string
	PaidAt        string
	Currency      string
	SellerDetails string
	VATNote       string
	ByPerson      bool
	Revision      int64
	CreatedAt     string
	Lines         []ExportInvoiceLine
}

type ExportInvoiceLine struct {
	Label       string
	Detail      string
	Seconds     int64
	RateCents   int64
	AmountCents int64
}

type ExportPayrollRun struct {
	ID          int64
	Number      string
	PeriodStart string
	PeriodEnd   string
	Status      string
	Notes       string
	Currency    string
	CreatedAt   string
	Lines       []ExportPayrollLine
}

type ExportPayrollLine struct {
	UserEmail   string
	Label       string
	Seconds     int64
	RateCents   int64
	AmountCents int64
}

type ExportSchedule struct {
	UserEmail string
	ProjectID int64
	Day       string
	Minutes   int64
	Note      string
	CreatedAt string
}

type ExportScheduleRow struct {
	UserEmail string
	ProjectID int64
	CreatedAt string
}

type ExportApproval struct {
	StartsAt        string
	EndsAt          string
	ApprovedByEmail string
	ApprovedAt      string
}

type ExportReport struct {
	Name           string
	Period         string
	ProjectID      int64
	TagID          int64
	ProjectSlug    string
	Tag            string
	CreatedByEmail string
	CreatedAt      string
}

// ExportWebhook keeps the endpoint and its events, never its signing secret.
type ExportWebhook struct {
	URL    string
	Events string
	Active bool
}

// ExportIntegration keeps the connection's name and settings, never its secret.
type ExportIntegration struct {
	Provider string
	Name     string
	Config   string
}

// WorkspaceRestoreRequest creates a new workspace from an export. The caller
// becomes its owner.
type WorkspaceRestoreRequest struct {
	CallerID int64
	Export   WorkspaceExport
}

// WorkspaceRestoreResult names the workspace a restore created.
type WorkspaceRestoreResult struct {
	TeamID int64
	Slug   string
}

// ErrUnsupportedWorkspaceFile means the file is not a paratrack workspace export
// this server can read.
var ErrUnsupportedWorkspaceFile = errors.New("not a paratrack workspace export of a supported version")

// ErrRestorePeopleMissing means the file names people this installation does not
// know. Nothing is written: each of them must sign up first.
var ErrRestorePeopleMissing = errors.New("people in the file are not registered here")
