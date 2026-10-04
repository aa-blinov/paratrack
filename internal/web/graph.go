package web

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/reportstats"
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
	graph, err := reportstats.HourlyGraph(sessions, period.Start, period.End, now)
	if err != nil {
		return ChartData{}, err
	}
	return chartDataFromGraph(graph, period, lang), nil
}

func chartDataFromGraph(graph appmodel.ReportGraphData, period timeparse.Period, lang i18n.Lang) ChartData {
	out := ChartData{
		Hours: make([]string, 24), Series: []chartSeries{}, Period: period.Label,
		HasData: len(graph.Series) > 0,
	}
	for hour := range out.Hours {
		out.Hours[hour] = fmtHourLabel(hour)
	}
	if !out.HasData {
		return out
	}
	out.TotalLabel = fmtDurL(lang, graph.TotalSeconds)
	for _, item := range graph.Series {
		series := chartSeries{Name: item.Name, Color: colorFor(item.Name), Data: make([]int, 24), Total: item.TotalMinutes}
		copy(series.Data, item.HourMinutes[:])
		out.Series = append(out.Series, series)
		out.Legend = append(out.Legend, chartLegendEntry{Name: item.Name, Color: series.Color})
	}
	return out
}

func fmtHourLabel(h int) string {
	if h == 0 || h == 24 {
		return "00"
	}
	return time.Date(0, 0, 0, h, 0, 0, 0, time.UTC).Format("15")
}
