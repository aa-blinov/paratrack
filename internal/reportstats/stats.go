// Package reportstats contains the transport-independent aggregation rules
// shared by report workflows and command-line presentation.
package reportstats

import (
	"fmt"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

type StatsBreakdown = appmodel.ReportStatsBreakdown
type ProjectStats = appmodel.ReportProjectStats
type StatsSummary = appmodel.ReportStatsSummary

// Summarize builds activity and project breakdowns from already filtered
// sessions. Callers retain responsibility for localized labels and formatting.
func Summarize(sessions []model.ActiveSession, projects map[int64]model.Project, from, to, now time.Time, uncategorized string) (StatsSummary, error) {
	type projectBucket struct {
		seconds    int
		activities map[string]int
	}

	activityTotals := make(map[string]int)
	projectTotals := make(map[int64]*projectBucket)
	var summary StatsSummary
	var err error
	for _, session := range sessions {
		seconds := session.Session.TrackedSecondsInWindow(from, to, now)
		if seconds <= 0 {
			continue
		}
		activityName := session.Activity.Name
		projectID := session.Activity.ProjectID
		activityTotals[activityName], err = money.AddInt(activityTotals[activityName], seconds)
		if err != nil {
			return StatsSummary{}, fmt.Errorf("sum activity %q: %w", activityName, err)
		}
		bucket := projectTotals[projectID]
		if bucket == nil {
			bucket = &projectBucket{activities: make(map[string]int)}
			projectTotals[projectID] = bucket
		}
		bucket.seconds, err = money.AddInt(bucket.seconds, seconds)
		if err != nil {
			return StatsSummary{}, fmt.Errorf("sum project %d time: %w", projectID, err)
		}
		bucket.activities[activityName], err = money.AddInt(bucket.activities[activityName], seconds)
		if err != nil {
			return StatsSummary{}, fmt.Errorf("sum project %d activity %q: %w", projectID, activityName, err)
		}
		summary.TotalSeconds, err = money.AddInt(summary.TotalSeconds, seconds)
		if err != nil {
			return StatsSummary{}, fmt.Errorf("sum tracked time: %w", err)
		}
	}

	summary.Activities = make([]StatsBreakdown, 0, len(activityTotals))
	for name, seconds := range activityTotals {
		summary.Activities = append(summary.Activities, StatsBreakdown{
			Name: name, Seconds: seconds, Share: shareOf(seconds, summary.TotalSeconds),
		})
	}
	sort.Slice(summary.Activities, func(i, j int) bool {
		if summary.Activities[i].Seconds == summary.Activities[j].Seconds {
			return summary.Activities[i].Name < summary.Activities[j].Name
		}
		return summary.Activities[i].Seconds > summary.Activities[j].Seconds
	})

	summary.Projects = make([]ProjectStats, 0, len(projectTotals))
	for id, bucket := range projectTotals {
		project := ProjectStats{ID: id, Name: uncategorized, Color: "#9ca3af", Seconds: bucket.seconds}
		if item, ok := projects[id]; ok {
			project.Name, project.Slug, project.Color = item.Name, item.Slug, item.Color
		}
		project.Share = shareOf(bucket.seconds, summary.TotalSeconds)
		project.Activities = make([]StatsBreakdown, 0, len(bucket.activities))
		for name, seconds := range bucket.activities {
			project.Activities = append(project.Activities, StatsBreakdown{
				Name: name, Seconds: seconds, Share: shareOf(seconds, summary.TotalSeconds),
			})
		}
		sort.Slice(project.Activities, func(i, j int) bool {
			if project.Activities[i].Seconds == project.Activities[j].Seconds {
				return project.Activities[i].Name < project.Activities[j].Name
			}
			return project.Activities[i].Seconds > project.Activities[j].Seconds
		})
		summary.Projects = append(summary.Projects, project)
	}
	sort.Slice(summary.Projects, func(i, j int) bool {
		if summary.Projects[i].Seconds == summary.Projects[j].Seconds {
			return summary.Projects[i].Name < summary.Projects[j].Name
		}
		return summary.Projects[i].Seconds > summary.Projects[j].Seconds
	})
	return summary, nil
}

func shareOf(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(value) / float64(total) * 100
}
