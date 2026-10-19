package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The utilization report sets each member's plan beside the tracked time. A
// member with a plan and no tracked time gets a row too, with zero time: that
// is the line a plan-against-actual comparison exists to show.
func TestUtilizationReportShowsPlanForMemberWithoutTime(t *testing.T) {
	e := newAPIEnv(t)
	e.register("util-plan@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Plan work"}}, nil))
	schedule := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(schedule.People) != 1 || len(schedule.Projects) != 1 {
		t.Fatalf("schedule has %d people and %d projects, want 1 and 1", len(schedule.People), len(schedule.Projects))
	}
	today := time.Now().Format("2006-01-02")
	resp := e.do("POST", "/api/schedule/cell", url.Values{
		"user_id":    {strconv.FormatInt(schedule.People[0].UserID, 10)},
		"project_id": {strconv.FormatInt(schedule.Projects[0].ID, 10)},
		"date":       {today},
		"minutes":    {"120"},
	}, map[string]string{"HX-Request": "true"})
	resp.Body.Close()

	from := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	report := reactData[reportRunPage](t, readBody(t, e.do("GET", "/reports/run?id=utilization&from="+from+"&to="+to, nil, nil)))
	if !report.VM.ShowPlan {
		t.Fatal("utilization report does not show the plan column")
	}
	found := false
	for _, row := range report.VM.Rows {
		if row.PlannedSecs == 7200 && row.Secs == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no zero-time row carries the 2 hour plan: %+v", report.VM.Rows)
	}
}
