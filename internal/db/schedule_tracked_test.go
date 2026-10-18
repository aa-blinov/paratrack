package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The schedule's tracked column counts what the week's sessions logged: a
// closed session inside the week, a running timer up to now, and nothing from
// the week before. A member with no time reads zero.
func TestScheduleTrackedCountsWeekAndOpenTimers(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	ownerID := seedApprovalUser(t, d, "tracked-owner@test.local")
	memberID := seedApprovalUser(t, d, "tracked-member@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "tracked-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}

	weekStart := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, activityID, weekStart.Add(10*time.Hour))                   // one hour, in the week
	seedClosedSession(t, d, teamID, ownerID, activityID, weekStart.AddDate(0, 0, -6).Add(10*time.Hour)) // last week, not counted

	// A timer started Wednesday 09:00 and still running at 09:30.
	started := weekStart.AddDate(0, 0, 2).Add(9 * time.Hour)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds, paused, last_resume_at)
		 VALUES (?, ?, ?, ?, NULL, 0, 0, ?)`,
		activityID, teamID, ownerID, FormatTime(started), FormatTime(started)); err != nil {
		t.Fatalf("seed running timer: %v", err)
	}

	rows, _, err := d.ListSchedule(ctx, appmodel.ScheduleQuery{TeamID: teamID, WeekStart: weekStart, Now: started.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("list schedule: %v", err)
	}
	tracked := make(map[int64]int, len(rows))
	for _, row := range rows {
		tracked[row.UserID] = row.Tracked
	}
	if got := tracked[ownerID]; got != 90 {
		t.Fatalf("owner tracked = %d minutes, want 90 (60 closed + 30 running)", got)
	}
	if got := tracked[memberID]; got != 0 {
		t.Fatalf("member tracked = %d minutes, want 0", got)
	}
}
