package web

import (
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/testutil"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func TestFmtDurationLadder(t *testing.T) {
	cases := map[int]string{
		-5:   "0m",
		0:    "0m",
		1:    "<1m",
		59:   "<1m",
		60:   "1m",
		90:   "1m",
		120:  "2m",
		3599: "59m",
		3600: "1h",
		5400: "1h 30m",
		7200: "2h",
	}
	for sec, want := range cases {
		if got := fmtDuration(sec); got != want {
			t.Errorf("fmtDuration(%d) = %q, want %q", sec, got, want)
		}
	}
}

func TestToSessionViewDurationSecsMatchesLabel(t *testing.T) {
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	sess := model.Session{ID: 1, ActivityID: 2, StartAt: start, EndAt: &end}
	act := model.Activity{ID: 2, Name: "reading"}
	v := toSessionView(sess, act, start.Add(-time.Hour), end.Add(time.Hour), time.Now(), i18n.En)
	if v.DurationSecs != 5400 {
		t.Errorf("DurationSecs = %d, want 5400", v.DurationSecs)
	}
	if v.Duration != "1h 30m" {
		t.Errorf("Duration = %q, want 1h 30m", v.Duration)
	}
}

func TestBuildChartDataTotalLabelUsesHoursNotMinutes(t *testing.T) {
	// 90 minutes of work must render as "1h 30m", not fmtDuration(90)="1m".
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	sessions := []appmodel.ActiveSession{{
		Session:  model.Session{StartAt: start, EndAt: &end},
		Activity: model.Activity{Name: "work"},
	}}
	period := timeparse.Period{Start: start.Add(-time.Hour), End: end.Add(time.Hour), Label: "today"}
	chart, err := buildChartData(sessions, period, end, i18n.En)
	if err != nil {
		t.Fatal(err)
	}
	if !chart.HasData {
		t.Fatal("expected chart data")
	}
	if chart.TotalLabel != "1h 30m" {
		t.Errorf("TotalLabel = %q, want 1h 30m", chart.TotalLabel)
	}
}

func TestBuildChartDataKeepsTrackedTimeWhenWallIntervalCollapses(t *testing.T) {
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	end := start
	sessions := []appmodel.ActiveSession{{
		Session:  model.Session{StartAt: start, EndAt: &end, AccumulatedSeconds: 600},
		Activity: model.Activity{Name: "imported"},
	}}
	period := timeparse.Period{Start: start.Add(-time.Hour), End: start.Add(time.Hour), Label: "today"}
	chart, err := buildChartData(sessions, period, end, i18n.En)
	if err != nil {
		t.Fatal(err)
	}
	if !chart.HasData || chart.Series[0].Data[start.Hour()] == 0 {
		t.Fatalf("collapsed interval hid tracked time: %+v", chart)
	}
}

func TestChartUsesTrackedTimeAndLocalHourBuckets(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+30*60)
	start := time.Date(2026, 9, 22, 10, 30, 0, 0, loc)
	end := start.Add(2 * time.Hour)
	period := timeparse.Period{Start: start.Add(-time.Hour), End: end.Add(time.Hour), Label: "today"}
	chart, err := buildChartData([]appmodel.ActiveSession{{
		Session:  model.Session{StartAt: start.UTC(), EndAt: &end, AccumulatedSeconds: 3600},
		Activity: model.Activity{Name: "paused-work"},
	}}, period, end, i18n.En)
	if err != nil {
		t.Fatal(err)
	}
	if chart.TotalLabel != "1h" || !chart.HasData {
		t.Fatalf("chart counts wall time instead of tracked time: %+v", chart)
	}
	series := chart.Series[0].Data
	if series[10] != 15 || series[11] != 30 || series[12] != 15 {
		t.Fatalf("local-hour distribution of 1 tracked hour: 10=%d 11=%d 12=%d", series[10], series[11], series[12])
	}
}

func TestPageMetaCoversGoalsAndTags(t *testing.T) {
	// Regression: render() used a closed type switch and dropped Title/Active
	// for goals/tags view-models.
	var _ pageMeta = dashboardData{pageData: pageData{Title: "Dashboard", Active: "dashboard"}}
	var _ pageMeta = statsData{pageData: pageData{Title: "Stats", Active: "stats"}}
	var _ pageMeta = graphData{pageData: pageData{Title: "Graph", Active: "graph"}}
	var _ pageMeta = tagsData{pageData: pageData{Title: "Tags", Active: "tags"}}
	goals := struct {
		pageData
	}{pageData: pageData{Title: "Goals", Active: "goals"}}
	var _ pageMeta = goals
	title, active := goals.pageInfo()
	if title != "Goals" || active != "goals" {
		t.Errorf("goals pageInfo = (%q, %q)", title, active)
	}
}

func TestSessionTagHTMXReturnsRowFragment(t *testing.T) {
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	srv, err := newServerForTest(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	// Unauthenticated API call must stay JSON 401 — the fragment path is
	// only for HTMX under a session. CSRF is checked first, so seed a
	// valid token to reach the auth gate.
	req := httptest.NewRequest("POST", "/api/sessions/1/tags", strings.NewReader("name=x"))
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	req.Header.Set(csrfHeaderName, csrfTok)
	req.AddCookie(csrfCk)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unauth tag add: status %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("unauth Content-Type = %q, want json", ct)
	}
}
func TestSessionMutationsScopedByTeam(t *testing.T) {
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	// Seed two workspaces owned by two users.
	for _, id := range []int64{1, 2} {
		email := "t" + string(rune('0'+id)) + "@x.test"
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, name) VALUES (?, ?, ?, ?)`,
			id, email, "x", "user"+string(rune('0'+id))); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO teams (id, name, slug, owner_id) VALUES (?, ?, ?, ?)`,
			id, "t"+string(rune('0'+id)), "t"+string(rune('0'+id)), id); err != nil {
			t.Fatalf("seed team %d: %v", id, err)
		}
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role) VALUES (1, 1, 'owner')`); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	ctx = requestctx.WithActor(ctx, 1)

	actA, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "work"})
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	sess, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: 1, ActivityID: actA.ID, At: time.Now(), Note: ""})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Team 2 must not see or mutate team 1's session.
	if _, err := d.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: 2, SessionID: sess.ID}); err == nil {
		t.Fatal("team 2 could read team 1 session")
	}
	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: 2, CallerID: 1, SessionID: sess.ID}); err == nil {
		t.Fatal("team 2 could delete team 1 session")
	}
	if _, err := d.UpdateSessionEnd(ctx, appmodel.TimerStopRequest{TeamID: 2, SessionID: sess.ID, At: time.Now()}); err == nil {
		t.Fatal("team 2 could stop team 1 session")
	}
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: 2, CallerID: 1, SessionID: sess.ID, Name: "sneaky"}); err == nil {
		t.Fatal("team 2 could tag team 1 session")
	}
	// Owner team still works.
	if _, err := d.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: 1, SessionID: sess.ID}); err != nil {
		t.Fatalf("team 1 lost access: %v", err)
	}
}

func TestFormatVeryLargeMinuteValuesWithoutSecondsOverflow(t *testing.T) {
	maxMinutes := int(^uint(0) >> 1)
	want := fmt.Sprintf("%dh %dm", maxMinutes/60, maxMinutes%60)
	if got := fmtMinutesL(i18n.En, maxMinutes); got != want {
		t.Fatalf("fmtMinutesL(max int) = %q, want %q", got, want)
	}
}
