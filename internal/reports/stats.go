package reports

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/reportstats"
)

// These aliases keep report workflow results compatible while the pure
// aggregation rules remain below the workflow layer.
type StatsBreakdown = reportstats.StatsBreakdown
type ProjectStats = reportstats.ProjectStats
type StatsSummary = reportstats.StatsSummary

// SummarizeStats is kept as the report package entrypoint for existing callers.
func SummarizeStats(sessions []model.ActiveSession, projects map[int64]model.Project, from, to, now time.Time, uncategorized string) (StatsSummary, error) {
	return reportstats.Summarize(sessions, projects, from, to, now, uncategorized)
}
