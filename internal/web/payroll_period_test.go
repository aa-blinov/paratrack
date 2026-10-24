package web

import (
	"net/url"
	"strings"
	"testing"
)

// A pay run whose period starts after it ends is refused on the payroll screen, and the
// dates typed come back in the form so one of them can be fixed.
func TestPayrollRefusalKeepsTheTypedPeriod(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payroll-period@x.test")
	readBody(t, e.do("POST", "/api/team/modules", url.Values{"preset": {"studio"}, "from": {"welcome"}}, nil))

	resp := e.do("POST", "/payroll", url.Values{"start": {"2026-10-25"}, "end": {"2026-10-24"}}, nil)
	readBody(t, resp)
	if resp.StatusCode != 303 {
		t.Fatalf("reversed pay period answered %d, want a redirect back to the payroll screen", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payroll?") || !strings.Contains(loc, "flash=") {
		t.Fatalf("redirect goes to %q, want /payroll with a flash", loc)
	}
	if !strings.Contains(loc, "start=2026-10-25") || !strings.Contains(loc, "end=2026-10-24") {
		t.Fatalf("redirect %q drops the typed period", loc)
	}
	page := readBody(t, e.do("GET", loc, nil, nil))
	if !strings.Contains(page, "Выберите корректный период: начало раньше конца.") {
		t.Fatal("the payroll screen does not say that the period starts after it ends")
	}
}
