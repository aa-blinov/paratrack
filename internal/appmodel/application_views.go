// Package appmodel defines transport-neutral application inputs and results.
package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type DashboardQuery struct {
	TeamID         int64
	Now            time.Time
	IncludeBilling bool
}

type DashboardSnapshot struct {
	Activities        []model.Activity
	ActiveSessions    []model.ActiveSession
	TodaySessions     []model.ActiveSession
	RecentSessions    []model.ActiveSession
	Projects          []model.Project
	TagsBySession     map[int64][]model.Tag
	ProjectsByID      map[int64]model.ProjectSummary
	Goals             []model.GoalProgress
	Unbilled          []model.UnbilledProject
	TodayStart        time.Time
	TodayEnd          time.Time
	RecentStart       time.Time
	RecentEnd         time.Time
	TodayTotalSeconds int
	TopActivityName   string
	HasSession        bool
}

// ActiveListSnapshot combines the tracking and workspace data needed to
// render the active-session list without exposing presentation types.
type ActiveListSnapshot struct {
	ActiveSessions []model.ActiveSession
	Projects       []model.Project
	TagsBySession  map[int64][]model.Tag
	ProjectsByID   map[int64]model.ProjectSummary
	FirstRun       bool
}

// SessionDecorationRequest selects the row metadata that a consumer needs
// for a batch of sessions.
type SessionDecorationRequest struct {
	TeamID          int64
	Sessions        []model.ActiveSession
	IncludeTags     bool
	IncludeProjects bool
}

// SessionDecorationSnapshot contains optional tag and project metadata keyed
// by session or project ID.
type SessionDecorationSnapshot struct {
	TagsBySession map[int64][]model.Tag
	ProjectsByID  map[int64]model.ProjectSummary
}

// ProjectListSnapshot combines projects with the workflow-calculated usage
// values displayed beside them.
type ProjectListSnapshot struct {
	Projects []model.Project
	Usage    map[int64]model.ProjectUsage
}

// ProjectCatalogSnapshot combines projects with their activity counts for
// adapters that do not need time-window usage totals.
type ProjectCatalogSnapshot struct {
	Projects       []model.Project
	ActivityCounts map[int64]int
}

// GoalManagementSnapshot combines the activity catalog and current progress
// used by the goal management page.
type GoalManagementSnapshot struct {
	Activities []model.Activity
	Progress   []model.GoalProgress
}

// TimerStopResult carries the stopped session and its duration as calculated
// at the stop request's timestamp.
type TimerStopResult struct {
	Session         model.Session
	DurationSeconds int
	ActivityName    string
}

// InvoiceDraftOptions combines workspace projects with their saved client
// details and records which projects may be selected for a new invoice.
type InvoiceDraftOptions struct {
	Projects    []InvoiceProjectOption
	HasBillable bool
}

type InvoiceProjectOption struct {
	Project   model.Project
	Client    model.ProjectClient
	HasClient bool
	Eligible  bool
}

type ReportLabels struct {
	Uncategorized string
	Unassigned    string
	FormerMember  string
}

type ReportBuildQuery struct {
	TeamID   int64
	From     time.Time
	To       time.Time
	Now      time.Time
	Location *time.Location
	GroupBy  string
	Billable bool
	Labels   ReportLabels
}

type ReportStatsQuery struct {
	TeamID        int64
	From          time.Time
	To            time.Time
	Now           time.Time
	PersonID      int64
	IncludePeople bool
	ProjectSlug   string
	Tag           string
	Uncategorized string
}

type ReportStatsResult struct {
	Sessions      []model.ActiveSession
	TagsBySession map[int64][]model.Tag
	ProjectsByID  map[int64]model.ProjectSummary
	People        []ReportPersonOption
	PersonFilter  int64
	Projects      []model.Project
	Project       model.Project
	Tags          []model.Tag
	Summary       ReportStatsSummary
}

type ReportPersonOption struct {
	UserID   int64
	Name     string
	Selected bool
}

type ReportGraphQuery struct {
	TeamID      int64
	From        time.Time
	To          time.Time
	Now         time.Time
	PersonID    int64
	ProjectSlug string
	Tag         string
}

type ReportGraphSeries struct {
	Name         string
	HourMinutes  [24]int
	TotalMinutes int
}

type ReportGraphData struct {
	TotalSeconds int
	Series       []ReportGraphSeries
}

type ReportGraphResult struct {
	Project      model.Project
	Graph        ReportGraphData
	PersonFilter int64
	PersonName   string
}

type ReportAggregateRow struct {
	Key         string
	Day         time.Time
	Seconds     int
	RateCents   int
	RateVaries  bool
	AmountCents int
	Currency    string
	Share       float64
}

type ReportAggregateResult struct {
	Rows         []ReportAggregateRow
	TotalSeconds int
	TotalCents   int
	ByCurrency   map[string]int
	TeamCurrency string
}

type ReportStatsBreakdown struct {
	Name    string
	Seconds int
	Share   float64
}

type ReportProjectStats struct {
	ID         int64
	Name       string
	Slug       string
	Color      string
	Seconds    int
	Share      float64
	Activities []ReportStatsBreakdown
}

type ReportStatsSummary struct {
	TotalSeconds int
	Activities   []ReportStatsBreakdown
	Projects     []ReportProjectStats
}

type UserPreferences struct {
	HiddenSections []string
	Tabs           []string
	Duration       string
	WeekStart      string
	TZ             string
	HiddenWidgets  []string
	DefaultProject map[string]int64
}

type ScheduleViewRow struct {
	model.ScheduleRow
	LoadPercent int
}

type ScheduleSnapshot struct {
	Rows         []ScheduleViewRow
	ProjectNames map[int64]string
	Projects     []model.Project
	TotalMinutes int
}

type IntegrationConnectResult struct {
	Integration model.IntegrationSummary
	Imported    int
	SyncError   error
}

type InvoiceStripePaymentLink struct {
	URL           string
	InvoiceNumber string
}

type InvoiceDraftCreation struct {
	Invoice       model.Invoice
	Lines         []model.InvoiceLine
	Overlaps      []string
	AdvisoryError error
}

// InvoiceDetailResult contains the invoice snapshot and workflow-calculated totals.
type InvoiceDetailResult struct {
	Invoice              model.Invoice
	Lines                []model.InvoiceLine
	TotalCents           int
	TotalHoursHundredths int
}

// InvoiceDocumentRequest scopes the shared invoice detail read used by the
// invoice page, printable documents, and outbound email.
type InvoiceDocumentRequest struct {
	TeamID                 int64
	InvoiceID              int64
	IncludeStripeReadiness bool
}

// InvoiceDocumentSnapshot combines invoice details with the workspace billing
// rules needed to render an invoice or act.
type InvoiceDocumentSnapshot struct {
	Details      InvoiceDetailResult
	BillingRules model.BillingRules
	StripeReady  bool
}

type InvoiceSummaryResult struct {
	Invoice              model.Invoice
	TotalCents           int
	TotalHoursHundredths int
}

// InvoiceIndexSnapshot contains the workflow data needed by the invoice list
// and draft form.
type InvoiceIndexSnapshot struct {
	Invoices     []InvoiceSummaryResult
	DraftOptions InvoiceDraftOptions
	Unbilled     []model.UnbilledProject
	Unassigned   []model.UnassignedActivity
}

// WebhookManagementSnapshot contains the credential-free data needed by the
// webhook settings page.
type WebhookManagementSnapshot struct {
	Endpoints  []WebhookEndpointView
	Deliveries map[int64][]WebhookDeliveryView
}

type WebhookEndpointView struct {
	ID     int64
	URL    string
	Events string
	Active bool
}

type WebhookDeliveryView struct {
	CreatedAt time.Time
	Event     string
	Status    int
	Error     string
}

// TeamMemberManagementSnapshot contains the member directory and payroll
// settings needed by the team members page.
type TeamMemberManagementSnapshot struct {
	Members     []model.TeamMember
	PaySettings []model.MemberPayrollSettings
}

// TeamInvitePageSnapshot contains the invitation and its workspace for the
// public invitation page. Empty values represent an unknown or deleted invite.
type TeamInvitePageSnapshot struct {
	Invite TeamInviteResult
	Team   model.Team
}

// IntegrationManagementSnapshot contains connected providers with task counts
// for the integration settings page.
type IntegrationManagementSnapshot struct {
	Items []IntegrationManagementItem
}

type IntegrationManagementItem struct {
	Integration model.IntegrationSummary
	TaskCount   int
}

// IntegrationDetailSnapshot contains one connection and its imported tasks.
type IntegrationDetailSnapshot struct {
	Integration model.IntegrationSummary
	Tasks       []model.ExternalTask
}

type PayrollRunSummary struct {
	Run                  model.PayrollRun
	TotalCents           int
	TotalHoursHundredths int
}

type PayrollRunDetail struct {
	Run                  model.PayrollRun
	Lines                []model.PayrollLine
	TotalCents           int
	TotalHoursHundredths int
}
