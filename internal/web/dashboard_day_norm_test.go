package web

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// The overview carries the daily norm for the label beside today's total.
func TestDashboardDayNorm(t *testing.T) {
	e := newAPIEnv(t)
	e.register("day-norm@x.test")
	page := reactData[dashboardData](t, readBody(t, e.do("GET", "/", nil, nil)))
	if want := fmtDurF(i18n.Ru, "hm", dayNormSeconds); page.DayNormLabel != want {
		t.Fatalf("day norm = %q, want %q", page.DayNormLabel, want)
	}
}
