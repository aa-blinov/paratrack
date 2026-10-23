package appmodel

import (
	"strconv"
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
	Sessions      []ActiveSession
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
	Key string
	// ActivityID, Project and Task tell apart activities that share a name, for the activity
	// grouping: a name is a label, not an identity (D-7, TIME-15). Task is the title of the
	// task the activity holds, if any (TIME-16).
	ActivityID int64
	Project    string
	Task       string
	// Person names the member of a day-by-person row (PLAN-07).
	Person string
	// UserID is set on member rows only (zero for other groupings and for
	// time with no member).
	UserID      int64
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
	// UnlinkedSeconds is the part of TotalSeconds on activities that no task holds.
	UnlinkedSeconds int
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

// ActivityLabels names each row of an activity grouping. Activities are told apart by
// id (D-7), so a name that more than one activity holds gets the id after it; a row
// whose name is unique keeps the name alone. Rows of one activity share a label.
func ActivityLabels(rows []ReportAggregateRow) []string {
	ids := make(map[string]map[int64]bool, len(rows))
	for _, row := range rows {
		if ids[row.Key] == nil {
			ids[row.Key] = make(map[int64]bool)
		}
		ids[row.Key][row.ActivityID] = true
	}
	labels := make([]string, len(rows))
	for i, row := range rows {
		labels[i] = row.Key
		if len(ids[row.Key]) > 1 {
			labels[i] = row.Key + " (#" + strconv.FormatInt(row.ActivityID, 10) + ")"
		}
	}
	return labels
}
