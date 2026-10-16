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
		Sessions: []appmodel.ActiveSession{
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
		Sessions: []appmodel.ActiveSession{
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
		Sessions: []appmodel.ActiveSession{
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
		Sessions: []appmodel.ActiveSession{
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

// A billable row is the sum of its activity lines: each line's hours are
// rounded to hundredths before pricing, as an invoice line is. Two activities
// of 15.08 h and 15.83 h at 4 500 RUB/h are 30.91 h and 139 095 RUB, not
// 30.92 h priced from the project's total.
func TestBillableRowsSumTheirLines(t *testing.T) {
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	session := func(activity int64, seconds int) appmodel.ActiveSession {
		return appmodel.ActiveSession{
			Session:  model.Session{StartAt: start, EndAt: timePtr(start.Add(time.Duration(seconds) * time.Second)), AccumulatedSeconds: seconds},
			Activity: model.Activity{ID: activity, Name: "activity", ProjectID: 1},
		}
	}
	input := AggregateInput{
		Sessions:          []appmodel.ActiveSession{session(17, 54300), session(18, 57000)},
		Projects:          map[int64]model.Project{1: {ID: 1, Billable: true, BillableRateCents: intPtr(450000)}},
		ProjectCurrencies: map[int64]string{1: "RUB"},
		TeamCurrency:      "RUB",
		GroupBy:           appmodel.ReportGroupProject,
		Billable:          true,
		From:              start.Add(-time.Hour),
		To:                start.Add(24 * time.Hour),
		Now:               start.Add(24 * time.Hour),
	}
	result, err := Aggregate(input)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	if got, want := result.Rows[0].Seconds, 3091*36; got != want {
		t.Fatalf("row seconds = %d, want %d (30.91 h)", got, want)
	}
	if got, want := result.Rows[0].AmountCents, 13909500; got != want {
		t.Fatalf("row amount = %d cents, want %d (139 095 RUB)", got, want)
	}
	if result.TotalCents != result.Rows[0].AmountCents || result.TotalSeconds != result.Rows[0].Seconds {
		t.Fatalf("total %d/%d differs from the only row %d/%d", result.TotalSeconds, result.TotalCents, result.Rows[0].Seconds, result.Rows[0].AmountCents)
	}

	// The workspace rounding rule applies to each line before it is priced:
	// 54 300 s rounds up to 61 quarter hours (15.25 h), 57 000 s to 64 (16 h).
	// Priced at 4 500 RUB/h that is 68 625 + 72 000 = 140 625 RUB.
	input.RoundSeconds = func(seconds int) int { return (seconds + 899) / 900 * 900 }
	rounded, err := Aggregate(input)
	if err != nil {
		t.Fatalf("aggregate with rounding: %v", err)
	}
	if got, want := rounded.Rows[0].AmountCents, 14062500; got != want {
		t.Fatalf("rounded row amount = %d cents, want %d (140 625 RUB)", got, want)
	}
}
