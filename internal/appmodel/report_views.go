package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

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
	GroupBy  ReportGroup
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
	ProjectsByID  map[int64]ProjectSummary
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
