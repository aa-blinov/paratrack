package web

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// "Approve everyone on track" approves, for a finished week, exactly the
// people at 80% of their norm with no empty weekday, and names them.
func TestApproveEveryoneOnTrack(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("on-track-owner@x.test")
	full := newAPIEnvSharedDB(t, owner)
	full.register("on-track-full@x.test")
	empty := newAPIEnvSharedDB(t, owner)
	empty.register("on-track-empty@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"on-track-full@x.test": full, "on-track-empty@x.test": empty}, "on-track-full@x.test", "on-track-empty@x.test")

	ctx := t.Context()
	sql := owner.db.TestSQL()
	var teamID, fullID, emptyID int64
	if err := sql.QueryRowContext(ctx, `SELECT m.team_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'on-track-full@x.test' AND m.role <> 'owner'`).Scan(&teamID, &fullID); err != nil {
		t.Fatal(err)
	}
	if err := sql.QueryRowContext(ctx, `SELECT id FROM users WHERE email = 'on-track-empty@x.test'`).Scan(&emptyID); err != nil {
		t.Fatal(err)
	}
	var actID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO activities (name, team_id) VALUES ('Работа', ?) RETURNING id`, teamID).Scan(&actID); err != nil {
		t.Fatal(err)
	}
	fortnight := time.Now().AddDate(0, 0, -14)
	monday := time.Date(fortnight.Year(), fortnight.Month(), fortnight.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(int(fortnight.Weekday())+6)%7)
	for i := 0; i < 5; i++ {
		start := monday.AddDate(0, 0, i).Add(9 * time.Hour)
		if _, err := sql.ExecContext(ctx,
			`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds) VALUES (?, ?, ?, ?, ?, 28800)`,
			actID, teamID, fullID, start.Format(time.RFC3339), start.Add(8*time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}

	// A Wednesday is taken to mean its week, not a span from Wednesday.
	resp := owner.do("POST", "/api/timesheet/approve-on-track", url.Values{"start": {monday.AddDate(0, 0, 2).Format("2006-01-02")}}, nil)
	resp.Body.Close()
	view := reactData[timesheetData](t, readBody(t, owner.do("GET", resp.Header.Get("Location"), nil, nil)))
	// Test accounts all register with the same display name, so the flash is
	// checked for its shape and one name; who was approved is read below.
	if !strings.HasPrefix(view.Flash, "Утверждены: ") || strings.Count(view.Flash, ",") != 0 {
		t.Fatalf("flash: %q", view.Flash)
	}
	approved := func(id int64) bool {
		var n int
		if err := sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ? AND user_id = ?`, teamID, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	if !approved(fullID) || approved(emptyID) {
		t.Fatalf("approved: full=%v empty=%v", approved(fullID), approved(emptyID))
	}
	var startsAt string
	if err := sql.QueryRowContext(ctx, `SELECT starts_at FROM timesheet_approvals WHERE team_id = ? AND user_id = ?`, teamID, fullID).Scan(&startsAt); err != nil {
		t.Fatal(err)
	}
	// Stored as an instant: Monday's midnight in the viewer's zone, which
	// is within a day of Monday in UTC, and nowhere near the Wednesday sent.
	at, err := time.Parse(time.RFC3339Nano, startsAt)
	if err != nil {
		t.Fatal(err)
	}
	if at.Before(monday.AddDate(0, 0, -1)) || !at.Before(monday.AddDate(0, 0, 1)) {
		t.Fatalf("the approval starts %s, want the Monday %s", startsAt, monday.Format("2006-01-02"))
	}
}
