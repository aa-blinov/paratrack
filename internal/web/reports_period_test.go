package web

import (
	"net/http"
	"strings"
	"testing"
)

// A report whose period starts after it ends is refused on the reports screen with
// the same sentence the payroll screen uses. It was a server error before.
func TestReportRunRefusesAPeriodThatEndsBeforeItStarts(t *testing.T) {
	e := newAPIEnv(t)
	e.register("reports-period@x.test")

	resp := e.do("GET", "/reports/run?id=by-project&from=2026-10-25&to=2026-10-24", nil, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reversed period answered %d, want a redirect back to the reports screen", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/reports?flash=") {
		t.Fatalf("redirect goes to %q, want /reports with a flash", loc)
	}
	page := readBody(t, e.do("GET", loc, nil, nil))
	if !strings.Contains(page, "Выберите корректный период: начало раньше конца.") {
		t.Fatal("the reports screen does not say that the period starts after it ends")
	}
}

// A period that ends on the day it starts is still a valid report.
func TestReportRunAcceptsASinglePlainDay(t *testing.T) {
	e := newAPIEnv(t)
	e.register("reports-day@x.test")

	resp := e.do("GET", "/reports/run?id=by-project&from=2026-10-24&to=2026-10-24", nil, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a one-day period answered %d, want 200", resp.StatusCode)
	}
}
