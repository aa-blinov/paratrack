package web

import (
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ChartData is the JSON payload passed from the Go template into the
// ECharts runtime. It collapses every session in the period into a
// 24-bucket stacked bar where X = hour of day (00..23) and the series
// are activities. This gives an "average hour-of-day" view that works
// for periods of any length without per-day faceting.
type ChartData struct {
	HasData    bool                `json:"hasData"`
	Hours      []string            `json:"hours"`      // 0..23 as zero-padded strings
	Series     []chartSeries       `json:"series"`     // one per activity
	TotalLabel string              `json:"totalLabel"` // "1d 04h 30m"
	Legend     []chartLegendEntry  `json:"legend"`
	Period     string              `json:"period"`
}

type chartSeries struct {
	Name   string `json:"name"`
	Color  string `json:"color"`
	Data   []int  `json:"data"` // minutes per hour, length 24
	Total  int    `json:"total"`
}

type chartLegendEntry struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// buildChartData aggregates the sessions into a 24-bucket distribution
// across activities. Sessions spanning multiple hours are split so a
// 10:30→13:45 session contributes 30 min to the 10:00 bucket, 60 to
// 11:00, 60 to 12:00 and 45 to 13:00.
func buildChartData(sessions []model.ActiveSession, period timeparse.Period) ChartData {
	out := ChartData{
		Hours:  make([]string, 24),
		Series: []chartSeries{},
		Period: period.Label,
	}
	for h := 0; h < 24; h++ {
		out.Hours[h] = fmtHourLabel(h)
	}

	// bucket[activityName] -> 24-element minutes array
	type activityBuckets struct {
		minutes [24]int
		total   int
	}
	buckets := map[string]*activityBuckets{}
	for _, as := range sessions {
		if as.Session.EndAt == nil {
			continue
		}
		s := as.Session.StartAt
		e := *as.Session.EndAt
		if e.Before(period.Start) || s.After(period.End) {
			continue
		}
		// Clip to the period window so a 3-day session in "today" period
		// doesn't leak time outside the visible range.
		if s.Before(period.Start) {
			s = period.Start
		}
		if e.After(period.End) {
			e = period.End
		}
		b, ok := buckets[as.Activity.Name]
		if !ok {
			b = &activityBuckets{}
			buckets[as.Activity.Name] = b
		}
		// Walk hour-by-hour along [s, e].
		cur := s
		for cur.Before(e) {
			nextHour := cur.Truncate(time.Hour).Add(time.Hour)
			endOfSegment := e
			if nextHour.Before(endOfSegment) {
				endOfSegment = nextHour
			}
			mins := int(endOfSegment.Sub(cur).Minutes())
			if mins > 0 {
				h := cur.Hour()
				b.minutes[h] += mins
				b.total += mins
			}
			cur = nextHour
		}
	}

	if len(buckets) == 0 {
		return ChartData{HasData: false, Hours: out.Hours, Period: period.Label}
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
	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })

	totalAll := 0
	for _, b := range buckets {
		totalAll += b.total
	}
	// buckets accumulate MINUTES; fmtDuration takes seconds.
	out.TotalLabel = fmtDuration(totalAll * 60)

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
	return out
}

func fmtHourLabel(h int) string {
	if h == 0 || h == 24 {
		return "00"
	}
	return time.Date(0, 0, 0, h, 0, 0, 0, time.UTC).Format("15")
}
