package db

import (
	"slices"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The fill-your-day nudge asks only the people with no session started on that day,
// and asks each of them once per day (the claim is the second guard).
func TestDayFillCandidatesAndClaimOncePerDay(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "day-fill")
	// 2031 is far from the dates the fixture seeds, so the day starts with no time.
	day := time.Date(2031, 3, 3, 0, 0, 0, 0, time.UTC)
	query := appmodel.DayFillCandidatesQuery{TeamID: ws.teamID, From: day, To: day.AddDate(0, 0, 1)}

	before, err := d.DayFillCandidates(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(before, ws.memberID) {
		t.Fatalf("candidates %v should hold the member with no time that day", before)
	}

	seedClosedSession(t, d, ws.teamID, ws.memberID, ws.activityID, day.Add(10*time.Hour))
	after, err := d.DayFillCandidates(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(after, ws.memberID) {
		t.Fatalf("candidates %v still hold the member who logged time that day", after)
	}

	claim := appmodel.DayFillNoticeRequest{TeamID: ws.teamID, UserID: ws.ownerID, Day: "2031-03-03"}
	first, err := d.ClaimDayFillNotice(t.Context(), claim)
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v; want true, nil", first, err)
	}
	second, err := d.ClaimDayFillNotice(t.Context(), claim)
	if err != nil || second {
		t.Fatalf("second claim for the same day = %v, %v; want false, nil", second, err)
	}
	claim.Day = "2031-03-04"
	nextDay, err := d.ClaimDayFillNotice(t.Context(), claim)
	if err != nil || !nextDay {
		t.Fatalf("claim for the next day = %v, %v; want true, nil", nextDay, err)
	}
}

// A blank zone and a zone the server does not know both read as UTC, so one bad
// setting cannot stop the reminders of the other workspaces.
func TestTeamLocationFallsBackToUTC(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "day-fill-zone")
	loc, err := d.TeamLocation(t.Context(), ws.teamID)
	if err != nil || loc.String() != "UTC" {
		t.Fatalf("blank zone = %v, %v; want UTC", loc, err)
	}
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE teams SET timezone = 'Europe/Moscow' WHERE id = ?`, ws.teamID); err != nil {
		t.Fatal(err)
	}
	loc, err = d.TeamLocation(t.Context(), ws.teamID)
	if err != nil || loc.String() != "Europe/Moscow" {
		t.Fatalf("Moscow = %v, %v", loc, err)
	}
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE teams SET timezone = 'Mars/Olympus' WHERE id = ?`, ws.teamID); err != nil {
		t.Fatal(err)
	}
	loc, err = d.TeamLocation(t.Context(), ws.teamID)
	if err != nil || loc.String() != "UTC" {
		t.Fatalf("unknown zone = %v, %v; want UTC", loc, err)
	}
}

// A member is asked on their own work days only, and everyone on a Saturday
// the workspace works by transfer.
func TestDayFillCandidatesFollowWorkDaysAndTransfers(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "day-fill-week")
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE memberships SET work_days = '123' WHERE team_id = ? AND user_id = ?`, ws.teamID, ws.memberID); err != nil {
		t.Fatal(err)
	}
	ask := func(day time.Time) []int64 {
		got, err := d.DayFillCandidates(t.Context(), appmodel.DayFillCandidatesQuery{TeamID: ws.teamID, From: day, To: day.AddDate(0, 0, 1), Day: day.Format("2006-01-02")})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	thursday := time.Date(2031, 3, 6, 0, 0, 0, 0, time.UTC)
	if got := ask(thursday); slices.Contains(got, ws.memberID) || !slices.Contains(got, ws.ownerID) {
		t.Fatalf("Thursday asked %v: the Monday-to-Wednesday member is off, the owner is not", got)
	}
	saturday := time.Date(2031, 3, 8, 0, 0, 0, 0, time.UTC)
	if got := ask(saturday); len(got) != 0 {
		t.Fatalf("an ordinary Saturday asked %v", got)
	}
	if _, err := d.TestSQL().ExecContext(t.Context(), `INSERT INTO team_holidays (team_id, day, name, kind) VALUES (?, '2031-03-08', 'перенос', 'work')`, ws.teamID); err != nil {
		t.Fatal(err)
	}
	if got := ask(saturday); !slices.Contains(got, ws.ownerID) {
		t.Fatalf("a worked Saturday asked %v", got)
	}
}
