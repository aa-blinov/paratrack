package web

import (
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// statsData feeds stats.html.
type personOpt struct {
	ID       int64
	Name     string
	Selected bool
}

type statsData struct {
	pageData
	Period        timeparse.Period
	Aggregated    []aggRow        // activity-level breakdown
	ByProject     []projectAggRow // project-grouped breakdown (with activities nested)
	Projects      []projectView   // for the project-filter chip row
	ProjectFilter string          // current ?project=slug value, empty if unfiltered
	People        []personOpt     // managers of a shared workspace: filter by person
	PersonFilter  int64
	Sessions      []sessionView
	Total         string
	SessionCount  int
	SessionsCut   bool // the log shows only the newest statsLogRows
	MeID          int64
	ShowAllURL    string   // this view with the whole log
	TagFilter     string   // current ?tag= value, empty if unfiltered
	AllTagNames   []string // for the inline-add input autocomplete
	SavedReports  []savedReportView
}

type aggRow struct {
	ActivityName string
	Color        string
	Duration     string
	Share        float64
}

// projectAggRow groups the activity-level breakdown under one
// project, so /stats can show "EORA RAG (45%)" with the activities
// nested underneath. Uncategorized activities appear in a single
// row with ProjectID == 0 and an empty Name.
type projectAggRow struct {
	ProjectID   int64
	ProjectName string // empty for "Uncategorized"
	Slug        string
	Color       string
	Duration    string
	Share       float64
	Activities  []aggRow
}

// graphData feeds graph.html.
type graphData struct {
	pageData
	GraphReact       bool
	Period           timeparse.Period
	PeriodStartInput string
	PeriodEndInput   string
	Chart            ChartData
	ChartJSON        string // pre-serialised JSON for the data-chart attribute
	ProjectFilter    string
	ProjectName      string
	TagFilter        string
	PersonFilter     int64
	PersonName       string
}
