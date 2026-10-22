package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The team week counts each member's time per day (PLAN-04). A session that runs past
// midnight is split between the two days, and an open timer counts up to now.
func TestTeamWeekSplitsSessionsAcrossDays(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Неделя", "team-week")
	ownerID := teamOwner(t, d, teamID)
	colleagueID := seedProjectTestUser(t, d, "week-colleague")
	addMember(t, d, teamID, colleagueID, "member")
	ownerActivity := newTestActivity(t, d, ctx, teamID, ownerID, "Вёрстка")
	colleagueActivity := newTestActivity(t, d, ctx, teamID, colleagueID, "Макет")

	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	days := []time.Time{monday, monday.AddDate(0, 0, 1), monday.AddDate(0, 0, 2)}
	seedClosedSession(t, d, teamID, ownerID, ownerActivity, monday.Add(9*time.Hour))
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds)
		 VALUES (?, ?, ?, ?, ?, 7200)`,
		colleagueActivity, teamID, colleagueID, FormatTime(monday.Add(23*time.Hour)), FormatTime(monday.Add(25*time.Hour))); err != nil {
		t.Fatal(err)
	}
	running := monday.AddDate(0, 0, 1).Add(10 * time.Hour)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds, paused, last_resume_at)
		 VALUES (?, ?, ?, ?, NULL, 0, 0, ?)`,
		ownerActivity, teamID, ownerID, FormatTime(running), FormatTime(running)); err != nil {
		t.Fatal(err)
	}

	week, err := d.TrackedSecondsByPersonDay(ctx, appmodel.PersonDayQuery{TeamID: teamID, Days: days, Now: running.Add(30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if got := week[ownerID]; len(got) != 3 || got[0] != 3600 || got[1] != 30*60 || got[2] != 0 {
		t.Fatalf("the owner's days = %v, want the hour on Monday and the half hour of the running timer on Tuesday", got)
	}
	if got := week[colleagueID]; len(got) != 3 || got[0] != 3600 || got[1] != 3600 || got[2] != 0 {
		t.Fatalf("the colleague's days = %v, want the session split at midnight into one hour and one hour", got)
	}
}
