package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A project line on the schedule survives a reload with no hours, and can be
// taken off only while it carries none.
func TestScheduleLineSurvivesWithoutHoursAndComesOffWhenEmpty(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "lines-owner@test.local")
	memberID := seedApprovalUser(t, d, "lines-member@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "lines-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Slug: "", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	week := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	line := appmodel.ScheduleLineRequest{TeamID: teamID, ActorID: ownerID, UserID: memberID, ProjectID: proj.ID}

	if err := d.AddScheduleLine(ctx, line); err != nil {
		t.Fatalf("add line: %v", err)
	}
	onSchedule := func() bool {
		rows, _, err := d.ListSchedule(ctx, appmodel.ScheduleQuery{TeamID: teamID, WeekStart: week, Now: week})
		if err != nil {
			t.Fatalf("list schedule: %v", err)
		}
		for _, row := range rows {
			if row.UserID == memberID {
				_, ok := row.ByProject[proj.ID]
				return ok
			}
		}
		return false
	}
	if !onSchedule() {
		t.Fatal("a line with no hours is not on the schedule")
	}

	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{TeamID: teamID, ActorID: ownerID, UserID: memberID, ProjectID: proj.ID, Day: week.AddDate(0, 0, 1), Minutes: 60}); err != nil {
		t.Fatalf("plan hours: %v", err)
	}
	if err := d.RemoveScheduleLine(ctx, line); !errors.Is(err, appmodel.ErrScheduleLineHasHours) {
		t.Fatalf("remove a line with hours = %v, want ErrScheduleLineHasHours", err)
	}
	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{TeamID: teamID, ActorID: ownerID, UserID: memberID, ProjectID: proj.ID, Day: week.AddDate(0, 0, 1), Minutes: 0}); err != nil {
		t.Fatalf("clear hours: %v", err)
	}
	if err := d.RemoveScheduleLine(ctx, line); err != nil {
		t.Fatalf("remove an empty line: %v", err)
	}
	if onSchedule() {
		t.Fatal("a removed line is still on the schedule")
	}
}
