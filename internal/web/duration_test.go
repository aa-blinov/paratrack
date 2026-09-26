package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func TestFmtDurationLadder(t *testing.T) {
	cases := map[int]string{
		-5:   "0m",
		0:    "0m",
		1:    "1m",
		59:   "1m",
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
	v := toSessionView(sess, act, start.Add(-time.Hour), end.Add(time.Hour), time.Now())
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
	sessions := []model.ActiveSession{{
		Session:  model.Session{StartAt: start, EndAt: &end},
		Activity: model.Activity{Name: "work"},
	}}
	period := timeparse.Period{Start: start.Add(-time.Hour), End: end.Add(time.Hour), Label: "today"}
	chart := buildChartData(sessions, period)
	if !chart.HasData {
		t.Fatal("expected chart data")
	}
	if chart.TotalLabel != "1h 30m" {
		t.Errorf("TotalLabel = %q, want 1h 30m", chart.TotalLabel)
	}
}

func TestFilterByTagKeepsOnlyTaggedRows(t *testing.T) {
	rows := []sessionView{
		{ID: 1, ActivityName: "a", DurationSecs: 60, Tags: []tagChip{{Name: "deep"}}},
		{ID: 2, ActivityName: "b", DurationSecs: 120},
	}
	got := filterByTag(rows, "deep")
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("filterByTag = %+v, want only row 1", got)
	}
	if got[0].DurationSecs != 60 {
		t.Errorf("DurationSecs lost in filter: %d", got[0].DurationSecs)
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
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	srv, err := New(d, "127.0.0.1:0")
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
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()

	// Seed two workspaces owned by two users.
	for _, id := range []int64{1, 2} {
		email := "t" + string(rune('0'+id)) + "@x.test"
		if _, err := d.SQL().ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, name) VALUES (?, ?, ?, ?)`,
			id, email, "x", "user"+string(rune('0'+id))); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
		if _, err := d.SQL().ExecContext(ctx,
			`INSERT INTO teams (id, name, slug, owner_id) VALUES (?, ?, ?, ?)`,
			id, "t"+string(rune('0'+id)), "t"+string(rune('0'+id)), id); err != nil {
			t.Fatalf("seed team %d: %v", id, err)
		}
	}

	actA, err := d.GetOrCreateActivity(ctx, 1, "work")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	sess, err := d.CreateSession(ctx, 1, actA.ID, time.Now(), "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Team 2 must not see or mutate team 1's session.
	if _, err := d.GetSession(ctx, 2, sess.ID); err == nil {
		t.Fatal("team 2 could read team 1 session")
	}
	if err := d.DeleteSession(ctx, 2, sess.ID); err == nil {
		t.Fatal("team 2 could delete team 1 session")
	}
	if _, err := d.UpdateSessionEnd(ctx, 2, sess.ID, time.Now()); err == nil {
		t.Fatal("team 2 could stop team 1 session")
	}
	if err := d.AttachTag(ctx, 2, sess.ID, "sneaky"); err == nil {
		t.Fatal("team 2 could tag team 1 session")
	}
	// Owner team still works.
	if _, err := d.GetSession(ctx, 1, sess.ID); err != nil {
		t.Fatalf("team 1 lost access: %v", err)
	}
}
