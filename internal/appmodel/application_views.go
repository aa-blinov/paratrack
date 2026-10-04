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
	ProjectSlug   string
	Tag           string
	Uncategorized string
}

type ReportStatsResult struct {
	Sessions      []model.ActiveSession
	TagsBySession map[int64][]model.Tag
	Projects      []model.Project
	Project       model.Project
	Tags          []model.Tag
	Summary       ReportStatsSummary
}

type ReportGraphQuery struct {
	TeamID      int64
	From        time.Time
	To          time.Time
	Now         time.Time
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
	Project model.Project
	Graph   ReportGraphData
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
