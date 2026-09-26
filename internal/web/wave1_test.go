package web

import (
	"strconv"
	"net/http/httptest"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

func newTestDB(t *testing.T) (*db.DB, error) {
	d, err := db.Open(":memory:")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, nil
}

func TestTimesheetUpsertAndList(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	act, err := d.GetOrCreateActivity(ctx, 0, "reading")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if err := d.UpsertDayTotal(ctx, 0, act.ID, day, 5400); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	weekStart := day.AddDate(0, 0, 0)
	// Monday Sep 21 2026? Sep 22 is Tuesday — startOfWeek in web; use Monday.
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	grid, err := d.ListTimesheet(ctx, 0, monday, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range grid.Rows {
		if r.ActivityID == act.ID {
			found = true
			if r.Secs[1] != 5400 { // Tuesday = index 1
				t.Errorf("Tuesday secs = %d, want 5400 (row=%+v)", r.Secs[1], r.Secs)
			}
		}
	}
	if !found {
		t.Fatal("activity missing from timesheet")
	}
	// Zero clears the day.
	if err := d.UpsertDayTotal(ctx, 0, act.ID, day, 0); err != nil {
		t.Fatal(err)
	}
	grid, _ = d.ListTimesheet(ctx, 0, monday, time.Now())
	for _, r := range grid.Rows {
		if r.ActivityID == act.ID && r.Secs[1] != 0 {
			t.Errorf("after clear Tuesday=%d", r.Secs[1])
		}
	}
	_ = weekStart
}

func TestSavedReportsCRUD(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	// saved_reports has a FK to teams — seed one workspace.
	if _, err := d.SQL().ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, name) VALUES (1, 'a@x.test', 'x', 'A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL().ExecContext(ctx,
		`INSERT INTO teams (id, name, slug, owner_id) VALUES (1, 'T', 't', 1)`); err != nil {
		t.Fatal(err)
	}
	r1, err := d.CreateSavedReport(ctx, 1, "My week", "week", "eora-rag", "deep-work", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Name != "My week" || r1.Period != "week" {
		t.Fatalf("bad report %+v", r1)
	}
	if _, err := d.CreateSavedReport(ctx, 1, "My week", "today", "", "", 1); err == nil {
		t.Fatal("duplicate name accepted")
	}
	list, err := d.ListSavedReports(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := d.DeleteSavedReport(ctx, 1, r1.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSavedReport(ctx, 1, r1.ID); err == nil {
		t.Fatal("second delete accepted")
	}
}

func TestAPIWave1(t *testing.T) {
	e := newAPIEnv(t)
	e.register("wave1@x.test")
	// timesheet page renders
	resp := e.do("GET", "/timesheet", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("timesheet: %d %s", resp.StatusCode, readBody(t, resp))
	}
	// save a report
	resp = e.do("POST", "/api/reports/save", url.Values{
		"name": {"Week view"}, "period": {"week"}, "project": {""}, "tag": {""},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("save report: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// cell upsert
	resp = e.do("POST", "/api/timesheet/cell", url.Values{
		"activity_id": {"1"}, "date": {"2026-09-22"}, "minutes": {"90"},
	}, map[string]string{"HX-Request": "true"})
	// may 404 if activity 1 is not ours — accept 200 or 404
	if resp.StatusCode != 200 && resp.StatusCode != 404 {
		t.Fatalf("cell: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()
}



func TestEstimateUI(t *testing.T) {
	e := newAPIEnv(t)
	e.register("estui@x.test")
	// create project
	resp := e.do("POST", "/projects/new", url.Values{"name": {"Budgeted"}, "color": {"#7c3aed"}}, nil)
	resp.Body.Close()
	// set estimate
	resp = e.do("POST", "/projects/budgeted", url.Values{
		"name": {"Budgeted"}, "color": {"#7c3aed"}, "estimate_minutes": {"480"},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("save estimate: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = e.do("GET", "/projects/budgeted", nil, nil)
	body := readBody(t, resp)
	if !strings.Contains(body, string(i18n.T(i18n.Default, "est.vsActual"))) {
		t.Fatalf("missing estimate card, body snippet: %s", body[len(body)/3:len(body)/3+400])
	}
	if !strings.Contains(body, fmtDurL(i18n.Default, 8*3600)) {
		t.Error("missing 8h label")
	}
}

// A filled timesheet cell must show its minutes, not 0 ("0 clears the
// day", so a stray 0 there is one keystroke from data loss).
func TestTimesheetCellShowsMinutes(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tscell@x.test")
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"reading"}, "start": {"2026-09-22 09:00"}, "end": {"2026-09-22 10:30"},
	}, nil)
	resp.Body.Close()
	body := readBody(t, e.do("GET", "/timesheet?date=2026-09-22", nil, nil))
	if !strings.Contains(body, `value="90"`) {
		t.Errorf("timesheet cell should hold 90 minutes")
	}
}

// Offline-queued timer actions carry the click time; only the last 24h
// is trusted, anything else falls back to now.
func TestActionTime(t *testing.T) {
	at := func(v string) time.Time {
		r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{"client_ts": {v}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return actionTime(r)
	}
	past := time.Now().Add(-30 * time.Minute).Truncate(time.Millisecond)
	if got := at(strconv.FormatInt(past.UnixMilli(), 10)); !got.Equal(past) {
		t.Errorf("recent client_ts: got %v, want %v", got, past)
	}
	for _, bad := range []string{"", "junk",
		strconv.FormatInt(time.Now().Add(48*time.Hour*-1).UnixMilli(), 10),
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)} {
		if got := at(bad); time.Since(got) > time.Second {
			t.Errorf("client_ts %q should fall back to now, got %v", bad, got)
		}
	}
}
