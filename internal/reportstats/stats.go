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
type GraphData = appmodel.ReportGraphData
type GraphSeries = appmodel.ReportGraphSeries

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

// HourlyGraph allocates tracked minutes into local hour-of-day buckets for a
// filtered set of report sessions. The returned totals retain exact seconds.
func HourlyGraph(sessions []model.ActiveSession, from, to, now time.Time) (GraphData, error) {
	type activityBuckets struct {
		minutes [24]int
		total   int
	}
	byActivity := make(map[string]*activityBuckets)
	result := GraphData{}
	for _, item := range sessions {
		if item.Session.EndAt == nil {
			continue
		}
		start, end := item.Session.StartAt, *item.Session.EndAt
		if end.Before(from) || start.After(to) {
			continue
		}
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		tracked := item.Session.TrackedSecondsInWindow(from, to, now)
		if !end.After(start) {
			tracked = item.Session.DurationSeconds(now)
		}
		if tracked <= 0 {
			continue
		}
		var err error
		result.TotalSeconds, err = money.AddInt(result.TotalSeconds, tracked)
		if err != nil {
			return GraphData{}, fmt.Errorf("sum chart tracked time: %w", err)
		}
		bucket := byActivity[item.Activity.Name]
		if bucket == nil {
			bucket = &activityBuckets{}
			byActivity[item.Activity.Name] = bucket
		}
		minutes := tracked / 60
		if tracked%60 >= 30 {
			minutes++
		}
		minutes = max(1, minutes)
		bucket.total, err = money.AddInt(bucket.total, minutes)
		if err != nil {
			return GraphData{}, fmt.Errorf("sum chart series %q: %w", item.Activity.Name, err)
		}
		start, end = start.In(from.Location()), end.In(from.Location())
		if !end.After(start) {
			bucket.minutes[start.Hour()], err = money.AddInt(bucket.minutes[start.Hour()], minutes)
			if err != nil {
				return GraphData{}, fmt.Errorf("sum chart hour %d for %q: %w", start.Hour(), item.Activity.Name, err)
			}
			continue
		}
		span := end.Sub(start)
		cursor, assigned := start, 0
		for cursor.Before(end) {
			nextHour := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), cursor.Hour()+1, 0, 0, 0, cursor.Location())
			if !nextHour.After(cursor) {
				nextHour = cursor.Add(time.Hour)
			}
			if nextHour.After(end) {
				nextHour = end
			}
			allocated := int(float64(minutes)*float64(nextHour.Sub(start))/float64(span) + 0.5)
			bucket.minutes[cursor.Hour()], err = money.AddInt(bucket.minutes[cursor.Hour()], allocated-assigned)
			if err != nil {
				return GraphData{}, fmt.Errorf("sum chart hour %d for %q: %w", cursor.Hour(), item.Activity.Name, err)
			}
			assigned = allocated
			cursor = nextHour
		}
	}
	rows := make([]GraphSeries, 0, len(byActivity))
	for name, buckets := range byActivity {
		rows = append(rows, GraphSeries{Name: name, HourMinutes: buckets.minutes, TotalMinutes: buckets.total})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalMinutes == rows[j].TotalMinutes {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].TotalMinutes > rows[j].TotalMinutes
	})
	result.Series = rows
	return result, nil
}

func shareOf(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(value) / float64(total) * 100
}
