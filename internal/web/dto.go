package web

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"

	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// pageData is the common envelope every HTML page needs: title for the
// <title> tag, the nav-highlight flag, and a pre-rendered ContentHTML
// fragment that base.html drops into the layout.
//
// Why pre-rendered: Go html/template does not allow `{{template .X .}}`
// with a dynamic name, so we render the page-specific block into a
// buffer first and pass the safe HTML into the layout.
type pageData struct {
	Title        string
	Active       string
	ContentHTML  template.HTML
}

// sessionView is the per-row representation of an active or recent
// session in the dashboard / stats tables.
type sessionView struct {
	ID                 int64
	ActivityID         int64
	ActivityName       string
	Color              string
	StartISO           string
	StartLocal         string
	EndLocal           string
	StartInput         string // value for datetime-local
	EndInput           string
	Duration           string
	DurationInput      string // user-editable representation ("1h 30m" or "HH:MM:SS")
	AccumulatedSeconds int
	Paused             bool
	Note               string
	Tags               []tagChip // attached tags, populated by hydrateSessionTags
}

// dashboardData feeds dashboard.html.
type dashboardData struct {
	pageData
	Activities []model.Activity
	ActiveSessions []sessionView
	Recent     []sessionView
	ActiveCount int
	TodayTotal string
	TopToday   string
	Goals      []goalView
}

// statsData feeds stats.html.
type statsData struct {
	pageData
	Period       timeparse.Period
	Aggregated   []aggRow
	Sessions     []sessionView
	Total        string
	SessionCount int
	TagFilter    string // current ?tag= value, empty if unfiltered
	AllTagNames  []string // for the inline-add input autocomplete
}

type aggRow struct {
	ActivityName string
	Color        string
	Duration     string
	Share        float64
}

// graphData feeds graph.html.
type graphData struct {
	pageData
	Period    timeparse.Period
	Chart     ChartData
	ChartJSON string // pre-serialised JSON for the data-chart attribute
}

// tagChip is the lightweight view-model for a tag in the stats row
// and on the /tags management page. We don't need the timestamps here.
type tagChip struct {
	ID   int64
	Name string
}

// tagsData feeds tags.html.
type tagsData struct {
	pageData
	Tags         []tagWithCount
	AllTagNames []string // for autocomplete on the new-tag input
}

// tagWithCount is a tag plus how many sessions carry it.
type tagWithCount struct {
	tagChip
	SessionCount int
}

// goalView is the per-row representation of a configured goal plus
// the progress actually achieved in its current window. Powers both the
// dashboard widget and the /goals management page.
type goalView struct {
	ID                int64
	ActivityName      string
	Color             string
	Period            string
	TargetMinutes     int
	TargetLabel       string // "2h", "1h 30m"
	AchievedMinutes   int
	AchievedLabel     string // "1h 32m"
	Percent           int    // 0..100+
	AchievedClass     string // "" | "met" | "exceeded"
	PeriodStartLabel  string // "Mon Sep 22"
	PeriodEndLabel    string // "Sun Sep 28"
	PeriodRangeLabel  string // short label e.g. "this week"
}

// -- view-model helpers ----------------------------------------------

func toSessionView(s model.Session, a model.Activity, periodStart, periodEnd time.Time, now time.Time) sessionView {
	v := sessionView{
		ID:                 s.ID,
		ActivityID:         s.ActivityID,
		ActivityName:       a.Name,
		Color:              colorFor(a.Name),
		StartISO:           s.StartAt.UTC().Format(time.RFC3339Nano),
		StartLocal:         s.StartAt.Local().Format("01-02 15:04"),
		AccumulatedSeconds: s.AccumulatedSeconds,
		Paused:             s.Paused,
	}
	if s.EndAt != nil {
		v.EndLocal = s.EndAt.Local().Format("01-02 15:04")
		v.EndInput = toLocalInput(*s.EndAt)
	}
	v.StartInput = toLocalInput(s.StartAt)
	if s.Note != nil {
		v.Note = *s.Note
	}
	// Compute the FULL duration first (for the editable input); the
	// clipped `Duration` (shown in the table cell) is derived after.
	if s.EndAt != nil {
		fullSecs := int(s.EndAt.Sub(s.StartAt).Seconds())
		v.DurationInput = durationToHuman(fullSecs)
		// Clipped for the display cell.
		start := s.StartAt
		end := *s.EndAt
		if periodStart.After(start) {
			start = periodStart
		}
		if periodEnd.Before(end) {
			end = periodEnd
		}
		if end.After(start) {
			v.Duration = fmtDuration(int(end.Sub(start).Seconds()))
		} else {
			v.Duration = "00:00:00"
		}
	} else if s.LastResumeAt != nil && !s.Paused {
		secs := s.DurationSeconds(now)
		v.Duration = fmtDuration(secs)
		v.DurationInput = durationToHuman(secs)
	} else if s.Paused {
		v.Duration = fmtDuration(s.AccumulatedSeconds)
		v.DurationInput = durationToHuman(s.AccumulatedSeconds)
	} else {
		v.Duration = "00:00:00"
		v.DurationInput = "0m"
	}
	return v
}

// hydrateSessionTags does a single batched lookup and attaches the
// resulting tag chips to each row. Safe to call with an empty slice.
func hydrateSessionTags(ctx context.Context, d *dbpkg.DB, rows []sessionView) {
	if len(rows) == 0 {
		return
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	tagsByID, err := d.TagsForSessions(ctx, ids)
	if err != nil {
		return // non-fatal — just skip rendering tags
	}
	for i := range rows {
		for _, t := range tagsByID[rows[i].ID] {
			rows[i].Tags = append(rows[i].Tags, tagChip{ID: t.ID, Name: t.Name})
		}
	}
}

// durationToHuman turns 5400 into "1h 30m" — friendlier for the
// duration input than "01:30:00".
func durationToHuman(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec / 60) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func fmtDuration(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, (sec/60)%60, sec%60)
}

func toLocalInput(t time.Time) string {
	// datetime-local wants "2006-01-02T15:04" with no zone and no seconds.
	return t.Local().Format("2006-01-02T15:04")
}

// shortSummary joins names with a comma for the dashboard's "Top today" line.
func shortSummary(name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(name)
}
