package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A manager sets the workspace's zone and week, every read of the workspace carries
// them back, and a member cannot change them (ADM-07).
func TestWorkspaceCalendarIsSetByManagersAndReadBack(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Календарь", "team-calendar")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "calendar-member")
	addMember(t, d, teamID, memberID, "member")

	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{TeamID: teamID, CallerID: memberID, TimeZone: "Asia/Tokyo", WeekStart: "sun"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member changed the calendar: %v", err)
	}
	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{TeamID: teamID, CallerID: ownerID, TimeZone: "Asia/Tokyo", WeekStart: "sun"}); err != nil {
		t.Fatal(err)
	}
	team, err := d.FindTeam(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if team.TimeZone != "Asia/Tokyo" || team.WeekStart != "sun" {
		t.Fatalf("the workspace reads back %q and %q", team.TimeZone, team.WeekStart)
	}
	teams, err := d.ListTeamsForUser(ctx, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0].TimeZone != "Asia/Tokyo" {
		t.Fatalf("a member's workspace list carries %+v, want the calendar", teams)
	}
}
