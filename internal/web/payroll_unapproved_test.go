package web

import (
	"net/url"
	"testing"
	"time"
)

// The payroll page names the finished weeks with tracked time that no approval
// covers, and drops them once the manager approves the week.
func TestPayrollPageNamesUnapprovedWeeksUntilApproved(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payroll-hint@x.test")
	fortnight := time.Now().AddDate(0, 0, -14)
	monday := fortnight.AddDate(0, 0, -(int(fortnight.Weekday())+6)%7)
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Вёрстка"},
		"start":    {fortnight.Format("2006-01-02") + " 09:00"},
		"end":      {fortnight.Format("2006-01-02") + " 11:00"},
	}, map[string]string{"HX-Request": "true"})
	resp.Body.Close()

	page := reactData[payrollPage](t, readBody(t, e.do("GET", "/payroll", nil, nil)))
	if len(page.UnapprovedWeeks) != 1 || page.UnapprovedWeeks[0].Date != monday.Format("2006-01-02") || page.UnapprovedWeeks[0].Count != 1 {
		t.Fatalf("payroll does not name the unapproved week: %+v", page.UnapprovedWeeks)
	}

	approve := e.do("POST", "/api/timesheet/approve", url.Values{
		"start": {monday.Format("2006-01-02")},
		"end":   {monday.AddDate(0, 0, 6).Format("2006-01-02")},
	}, nil)
	approve.Body.Close()
	if approve.StatusCode != 303 {
		t.Fatalf("approve the week: status %d", approve.StatusCode)
	}
	page = reactData[payrollPage](t, readBody(t, e.do("GET", "/payroll", nil, nil)))
	if len(page.UnapprovedWeeks) != 0 {
		t.Fatalf("approved week still listed: %+v", page.UnapprovedWeeks)
	}
}

// The payroll page's default period ends on the last finished day, so a fresh
// visit does not start with a period the current week would refuse.
func TestPayrollDefaultPeriodEndsBeforeTheCurrentWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payroll-default@x.test")
	page := reactData[payrollPage](t, readBody(t, e.do("GET", "/payroll", nil, nil)))
	now := time.Now()
	lastFinished := startOfWeek(nil, now).AddDate(0, 0, -1)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	want := now.Format("2006-01-02")
	if !lastFinished.Before(monthStart) {
		want = lastFinished.Format("2006-01-02")
	}
	if page.DefEnd != want {
		t.Fatalf("default period ends %s, want %s", page.DefEnd, want)
	}
}
