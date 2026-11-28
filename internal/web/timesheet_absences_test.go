package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A holiday and a vacation leave the norm: someone who worked every day but
// the holiday, and someone away the whole week, are both on track and both
// approved by "approve everyone on track"; without the holiday the first
// would have an empty weekday.
func TestAbsencesAndHolidaysLeaveTheNorm(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("absence-owner@x.test")
	worker := newAPIEnvSharedDB(t, owner)
	worker.register("absence-worker@x.test")
	away := newAPIEnvSharedDB(t, owner)
	away.register("absence-away@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"absence-worker@x.test": worker, "absence-away@x.test": away}, "absence-worker@x.test", "absence-away@x.test")

	ctx := t.Context()
	sql := owner.db.TestSQL()
	var teamID, workerID, awayID int64
	if err := sql.QueryRowContext(ctx, `SELECT m.team_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'absence-worker@x.test' AND m.role <> 'owner'`).Scan(&teamID, &workerID); err != nil {
		t.Fatal(err)
	}
	if err := sql.QueryRowContext(ctx, `SELECT id FROM users WHERE email = 'absence-away@x.test'`).Scan(&awayID); err != nil {
		t.Fatal(err)
	}
	var actID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO activities (name, team_id) VALUES ('Работа', ?) RETURNING id`, teamID).Scan(&actID); err != nil {
		t.Fatal(err)
	}
	fortnight := time.Now().AddDate(0, 0, -14)
	monday := time.Date(fortnight.Year(), fortnight.Month(), fortnight.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(int(fortnight.Weekday())+6)%7)
	for _, i := range []int{0, 1, 3, 4} { // Wednesday is the holiday
		start := monday.AddDate(0, 0, i).Add(9 * time.Hour)
		if _, err := sql.ExecContext(ctx,
			`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds) VALUES (?, ?, ?, ?, ?, 28800)`,
			actID, teamID, workerID, start.Format(time.RFC3339), start.Add(8*time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	week := monday.Format("2006-01-02")
	status := func() map[int64]timesheetTeamMemberStatus {
		t.Helper()
		view := reactData[timesheetData](t, readBody(t, owner.do("GET", "/timesheet?date="+week, nil, nil)))
		out := map[int64]timesheetTeamMemberStatus{}
		for _, m := range view.TeamSummary {
			out[m.UserID] = m
		}
		return out
	}
	if s := status()[workerID]; s.Status != "warn" || s.MissingDays != 1 {
		t.Fatalf("before the holiday the worker has an empty Wednesday: %+v", s)
	}

	wednesday := monday.AddDate(0, 0, 2).Format("2006-01-02")
	owner.do("POST", "/api/holidays", url.Values{"week": {week}, "from": {wednesday}, "name": {"Праздник"}}, nil).Body.Close()
	owner.do("POST", "/api/absences", url.Values{"week": {week}, "user_id": {strconv.FormatInt(awayID, 10)}, "kind": {"vacation"},
		"from": {week}, "to": {monday.AddDate(0, 0, 6).Format("2006-01-02")}}, nil).Body.Close()

	after := status()
	if s := after[workerID]; s.Status != "ok" || s.MissingDays != 0 || s.Percent != 100 {
		t.Fatalf("with the holiday the worker is on track: %+v", s)
	}
	if s := after[awayID]; s.Status != "away" || s.AwayLabel == "" {
		t.Fatalf("a week of vacation is away: %+v", s)
	}

	// The member's own mark lives apart: clearing or setting it on a day of
	// their vacation leaves the vacation as the manager recorded it.
	away.do("POST", "/api/day-marks", url.Values{"day": {week}, "kind": {""}}, nil).Body.Close()
	away.do("POST", "/api/day-marks", url.Values{"day": {week}, "kind": {"idle"}}, nil).Body.Close()
	var kept int
	if err := sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM member_absences WHERE team_id = ? AND user_id = ?`, teamID, awayID).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 7 {
		t.Fatalf("the member's own mark changed their vacation: %d days left", kept)
	}

	owner.do("POST", "/api/timesheet/approve-on-track", url.Values{"start": {week}}, nil).Body.Close()
	approved := func(id int64) bool {
		var n int
		if err := sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ? AND user_id = ?`, teamID, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	if !approved(workerID) || !approved(awayID) {
		t.Fatalf("approved: worker=%v away=%v", approved(workerID), approved(awayID))
	}
}
