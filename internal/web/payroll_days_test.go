package web

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// A payroll line breaks down into the days the person logged, and only those days (FIN-18).
// A run too long to break down shows its totals only.
func TestPayrollBreakdownKeepsDaysWithTime(t *testing.T) {
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	days := runDays(start, start.AddDate(0, 0, 3))
	if len(days) != 3 {
		t.Fatalf("a three-day run covers %d days, want 3 (the end is exclusive)", len(days))
	}
	got := payrollDays(i18n.Ru, days, []int{3600, 0, 1800})
	if len(got) != 2 || got[0].Day != fmtDay(i18n.Ru, days[0]) || got[1].Day != fmtDay(i18n.Ru, days[2]) {
		t.Fatalf("breakdown %+v, want the first and the third day only", got)
	}
	if got[0].Hours == "" || got[1].Hours == "" {
		t.Fatalf("breakdown carries no hours: %+v", got)
	}
	if long := runDays(start, start.AddDate(0, 0, 400)); long != nil {
		t.Fatalf("a 400-day run got a per-day breakdown of %d days", len(long))
	}
}
