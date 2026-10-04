package web

import (
	"fmt"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ChartData is the JSON payload passed from the Go template into the
// ECharts runtime. It collapses every session in the period into a
// 24-bucket stacked bar where X = hour of day (00..23) and the series
// are activities. This gives an "average hour-of-day" view that works
// for periods of any length without per-day faceting.
type ChartData struct {
	HasData    bool               `json:"hasData"`
	Hours      []string           `json:"hours"`      // 0..23 as zero-padded strings
	Series     []chartSeries      `json:"series"`     // one per activity
	TotalLabel string             `json:"totalLabel"` // "1d 04h 30m"
	Legend     []chartLegendEntry `json:"legend"`
	Period     string             `json:"period"`
}

type chartSeries struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Data  []int  `json:"data"` // minutes per hour, length 24
	Total int    `json:"total"`
}

type chartLegendEntry struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// buildChartData aggregates the sessions into a 24-bucket distribution
// across activities. Sessions spanning multiple hours are split so a
// 10:30→13:45 session contributes 30 min to the 10:00 bucket, 60 to
// 11:00, 60 to 12:00 and 45 to 13:00.
func buildChartData(sessions []model.ActiveSession, period timeparse.Period, now time.Time, lang i18n.Lang) (ChartData, error) {
	out := ChartData{
		Hours:  make([]string, 24),
		Series: []chartSeries{},
		Period: period.Label,
	}
	for h := 0; h < 24; h++ {
		out.Hours[h] = fmtHourLabel(h)
	}

	// Distribute tracked (non-paused) time over the visible wall interval.
	// Buckets display whole minutes, but the total keeps exact seconds.
	type activityBuckets struct {
		minutes [24]int
		total   int
	}
	buckets := map[string]*activityBuckets{}
	totalSeconds := 0
	for _, as := range sessions {
		if as.Session.EndAt == nil {
			continue
		}
		s := as.Session.StartAt
		e := *as.Session.EndAt
		if e.Before(period.Start) || s.After(period.End) {
			continue
		}
		if s.Before(period.Start) {
			s = period.Start
		}
		if e.After(period.End) {
			e = period.End
		}
		tracked := as.Session.TrackedSecondsInWindow(period.Start, period.End, now)
		if !e.After(s) {
			// A hand-edited/imported session may have a zero wall-clock span.
			tracked = as.Session.DurationSeconds(now)
		}
		if tracked <= 0 {
			continue
		}
		var err error
		totalSeconds, err = money.AddInt(totalSeconds, tracked)
		if err != nil {
			return ChartData{}, fmt.Errorf("sum chart tracked time: %w", err)
		}
		b := buckets[as.Activity.Name]
		if b == nil {
			b = &activityBuckets{}
			buckets[as.Activity.Name] = b
		}
		minutes := tracked / 60
		if tracked%60 >= 30 {
			minutes++
		}
		minutes = max(1, minutes) // chart resolution: nearest minute
		b.total, err = money.AddInt(b.total, minutes)
		if err != nil {
			return ChartData{}, fmt.Errorf("sum chart series %q: %w", as.Activity.Name, err)
		}
		// The chart is explicitly about the user's hours, not UTC hours.
		s, e = s.In(period.Start.Location()), e.In(period.Start.Location())
		if !e.After(s) {
			b.minutes[s.Hour()], err = money.AddInt(b.minutes[s.Hour()], minutes)
			if err != nil {
				return ChartData{}, fmt.Errorf("sum chart hour %d for %q: %w", s.Hour(), as.Activity.Name, err)
			}
			continue
		}
		span := e.Sub(s)
		cur, assigned := s, 0
		for cur.Before(e) {
			end := time.Date(cur.Year(), cur.Month(), cur.Day(), cur.Hour()+1, 0, 0, 0, cur.Location())
			if !end.After(cur) { // daylight-saving fall-back hour
				end = cur.Add(time.Hour)
			}
			if end.After(e) {
				end = e
			}
			// Cumulative rounding guarantees every session contributes exactly
			// its tracked minutes across the 24 buckets.
			allocated := int(float64(minutes)*float64(end.Sub(s))/float64(span) + 0.5)
			b.minutes[cur.Hour()], err = money.AddInt(b.minutes[cur.Hour()], allocated-assigned)
			if err != nil {
				return ChartData{}, fmt.Errorf("sum chart hour %d for %q: %w", cur.Hour(), as.Activity.Name, err)
			}
			assigned = allocated
			cur = end
		}
	}

	if len(buckets) == 0 {
		return ChartData{HasData: false, Hours: out.Hours, Period: period.Label}, nil
	}

	// Sort activities by total desc for stable legend / series order.
	type row struct {
		name  string
		total int
	}
	rows := make([]row, 0, len(buckets))
	for name, b := range buckets {
		rows = append(rows, row{name, b.total})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].total == rows[j].total {
			return rows[i].name < rows[j].name
		}
		return rows[i].total > rows[j].total
	})

	// The displayed total uses tracked seconds, not rounded bar heights.
	out.TotalLabel = fmtDurL(lang, totalSeconds)

	for _, r := range rows {
		b := buckets[r.name]
		s := chartSeries{
			Name:  r.name,
			Color: colorFor(r.name),
			Data:  make([]int, 24),
			Total: b.total,
		}
		copy(s.Data, b.minutes[:])
		out.Series = append(out.Series, s)
		out.Legend = append(out.Legend, chartLegendEntry{Name: r.name, Color: s.Color})
	}
	out.HasData = true
	return out, nil
}

func fmtHourLabel(h int) string {
	if h == 0 || h == 24 {
		return "00"
	}
	return time.Date(0, 0, 0, h, 0, 0, 0, time.UTC).Format("15")
}
