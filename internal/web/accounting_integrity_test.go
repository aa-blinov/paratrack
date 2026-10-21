package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Reopening a week that a pay run covers is refused, and the refusal reaches the
// timesheet as a flash that names the reason. Once the pay run is gone the same
// reopen succeeds and says so.
func TestTimesheetShowsWhyAPaidWeekCannotBeReopened(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("paid-week-flash@x.test")
	form := url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}

	approved := owner.do("POST", "/api/timesheet/approve", form, nil)
	approved.Body.Close()

	ctx := context.Background()
	if _, err := owner.db.TestSQL().ExecContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, currency, created_at)
		 SELECT team_id, 'PAY-WEB-FLASH', '2020-03-02T00:00:00Z', '2020-03-09T00:00:00Z', 'paid', '', 'RUB', '2020-03-10T00:00:00Z'
		 FROM memberships WHERE role = 'owner' LIMIT 1`); err != nil {
		t.Fatalf("seed pay run: %v", err)
	}

	refused := owner.do("POST", "/api/timesheet/reopen", form, nil)
	refused.Body.Close()
	if refused.StatusCode != 303 {
		t.Fatalf("reopen a paid week: status %d, want a redirect", refused.StatusCode)
	}
	page := reactData[timesheetData](t, readBody(t, owner.do("GET", refused.Header.Get("Location"), nil, nil)))
	const why = "По этой неделе уже есть выплата, утверждение снять нельзя."
	if page.Flash != why || page.FlashOK {
		t.Fatalf("the timesheet shows flash %q (ok %v), want the refusal %q", page.Flash, page.FlashOK, why)
	}

	if _, err := owner.db.TestSQL().ExecContext(ctx, `DELETE FROM payroll_runs WHERE number = 'PAY-WEB-FLASH'`); err != nil {
		t.Fatalf("remove pay run: %v", err)
	}
	reopened := owner.do("POST", "/api/timesheet/reopen", form, nil)
	reopened.Body.Close()
	done := reactData[timesheetData](t, readBody(t, owner.do("GET", reopened.Header.Get("Location"), nil, nil)))
	if strings.TrimSpace(done.Flash) == "" || !done.FlashOK {
		t.Fatalf("a reopened week shows flash %q (ok %v), want a success", done.Flash, done.FlashOK)
	}
}
