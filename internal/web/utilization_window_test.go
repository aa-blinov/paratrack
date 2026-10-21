package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// The default window of the team load report is the current week, so the days
// still to come are in it. A Wednesday and the Sunday after it give the same week.
func TestUtilizationDefaultWindowIsTheCurrentWeek(t *testing.T) {
	r := httptest.NewRequest("GET", "/reports/run?id=utilization", nil)
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	nextMonday := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

	for _, now := range []time.Time{
		time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), // Wednesday
		time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC), // Sunday, the last day of the week
	} {
		from, to := currentWeekWindow(r, now)
		if !from.Equal(monday) || !to.Equal(nextMonday) {
			t.Fatalf("window for %v = %v..%v, want %v..%v", now, from, to, monday, nextMonday)
		}
	}
}

// A week with its plan on the last day must sum that day too. The plan is 30 hours
// spread over the week, and the report over the whole week shows all 30.
func TestUtilizationPlanCoversEveryDayOfTheWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("utilization-plan-week@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"План недели"}}, nil))

	ctx := context.Background()
	var project, team, owner int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM projects WHERE name = 'План недели'`).Scan(&project); err != nil {
		t.Fatalf("find the project: %v", err)
	}
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT team_id, user_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&team, &owner); err != nil {
		t.Fatalf("find the owner: %v", err)
	}
	for _, day := range []string{"2020-03-04", "2020-03-06", "2020-03-08"} {
		if _, err := e.db.TestSQL().ExecContext(ctx,
			`INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
			 VALUES (?, ?, ?, ?, 600, '', '2020-03-01T00:00:00Z')`,
			team, owner, project, day); err != nil {
			t.Fatalf("plan %s: %v", day, err)
		}
	}

	page := reactData[struct{ VM reportVM }](t, readBody(t, e.do("GET", "/reports/run?id=utilization&from=2020-03-02&to=2020-03-08", nil, nil)))
	planned := 0
	for _, row := range page.VM.Rows {
		planned += row.PlannedSecs
	}
	if planned != 30*60*60 {
		t.Fatalf("the week's plan reads %d seconds, want %d (30 hours)", planned, 30*60*60)
	}
}
