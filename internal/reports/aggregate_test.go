package reports

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestAggregateSplitsBillableTotalsByCurrency(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	input := AggregateInput{
		Sessions: []model.ActiveSession{
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "design", ProjectID: 1}},
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "design", ProjectID: 2}},
		},
		Projects: map[int64]model.Project{
			1: {ID: 1, Billable: true, BillableRateCents: intPtr(1200)},
			2: {ID: 2, Billable: true, BillableRateCents: intPtr(2500)},
		},
		ProjectCurrencies: map[int64]string{1: "USD", 2: "EUR"},
		TeamCurrency:      "USD",
		GroupBy:           appmodel.ReportGroupActivity,
		Billable:          true,
		From:              start.Add(-time.Hour),
		To:                start.Add(2 * time.Hour),
		Now:               start.Add(2 * time.Hour),
	}

	result, err := Aggregate(input)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if result.TotalSeconds != 7200 || len(result.Rows) != 2 {
		t.Fatalf("aggregate = %d seconds across %d rows, want 7200 across 2", result.TotalSeconds, len(result.Rows))
	}
	if result.ByCurrency["USD"] != 1200 || result.ByCurrency["EUR"] != 2500 {
		t.Fatalf("amounts by currency = %#v, want USD 1200 and EUR 2500", result.ByCurrency)
	}
	if result.TotalCents != 3700 {
		t.Fatalf("total cents = %d, want 3700", result.TotalCents)
	}
}

func TestAggregateMarksMixedRatesInGroupedRows(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	result, err := Aggregate(AggregateInput{
		Sessions: []model.ActiveSession{
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "design", ProjectID: 1}},
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "design", ProjectID: 2}},
		},
		Projects: map[int64]model.Project{
			1: {ID: 1, Billable: true, BillableRateCents: intPtr(1200)},
			2: {ID: 2, Billable: true, BillableRateCents: intPtr(2500)},
		},
		ProjectCurrencies: map[int64]string{1: "USD", 2: "USD"},
		TeamCurrency:      "USD",
		GroupBy:           appmodel.ReportGroupActivity,
		Billable:          true,
		From:              start.Add(-time.Hour),
		To:                start.Add(2 * time.Hour),
		Now:               start.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 1 || !result.Rows[0].RateVaries || result.Rows[0].RateCents != 0 {
		t.Fatalf("grouped rate = %#v, want one row explicitly marked as mixed", result.Rows)
	}
	if result.Rows[0].AmountCents != 3700 {
		t.Fatalf("grouped amount = %d, want 3700 cents", result.Rows[0].AmountCents)
	}
}

func TestAggregateGroupsDaysInRequestedLocation(t *testing.T) {
	location := time.FixedZone("UTC-7", -7*60*60)
	first := time.Date(2026, 9, 2, 0, 30, 0, 0, time.UTC)
	second := time.Date(2026, 9, 2, 6, 30, 0, 0, time.UTC)
	result, err := Aggregate(AggregateInput{
		Sessions: []model.ActiveSession{
			{Session: model.Session{StartAt: first, EndAt: timePtr(first.Add(time.Hour)), AccumulatedSeconds: 3600}},
			{Session: model.Session{StartAt: second, EndAt: timePtr(second.Add(time.Hour)), AccumulatedSeconds: 3600}},
		},
		GroupBy: appmodel.ReportGroupDay, From: first.Add(-time.Hour), To: second.Add(2 * time.Hour),
		Now: second.Add(2 * time.Hour), Location: location, TeamCurrency: "USD",
	})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Day.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("day buckets = %#v, want one bucket for 2026-09-01 local time", result.Rows)
	}
}

func TestAggregateGroupsAllActivitiesIntoDay(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	result, err := Aggregate(AggregateInput{
		Sessions: []model.ActiveSession{
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "design"}},
			{Session: model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "review"}},
		},
		GroupBy: appmodel.ReportGroupDay, From: start.Add(-time.Hour), To: start.Add(2 * time.Hour),
		Now: start.Add(2 * time.Hour), TeamCurrency: "USD",
	})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Seconds != 7200 {
		t.Fatalf("day rows = %+v, want one row totaling 7200 seconds", result.Rows)
	}
}

func TestAggregateRejectsUnknownGroup(t *testing.T) {
	_, err := Aggregate(AggregateInput{GroupBy: appmodel.ReportGroup("unknown")})
	if err != ErrInvalidReportQuery {
		t.Fatalf("aggregate with unknown group error = %v, want %v", err, ErrInvalidReportQuery)
	}
}

func timePtr(value time.Time) *time.Time { return &value }
func intPtr(value int) *int              { return &value }
