package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Reassigning a task can carry the old assignee's plan on the project from today on
// to the new one, day by day. Past days stay where they were, and only a person who
// can plan the project may move it (TASK-03).
func TestMovePlanCarriesFutureDaysToTheNewAssignee(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "План", "plan-move")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	var newID int64
	if err := d.sql.QueryRowContext(ctx, `INSERT INTO users (email, password_hash, name) VALUES ('plan-newcomer@x.test', 'x', 'Новый') RETURNING id`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sql.ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (?, ?, 'member')`, teamID, newID); err != nil {
		t.Fatal(err)
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	plan := func(day time.Time, minutes int) {
		t.Helper()
		if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{TeamID: teamID, ActorID: ownerID, UserID: ownerID, ProjectID: proj.ID, Day: day, Minutes: minutes}); err != nil {
			t.Fatal(err)
		}
	}
	plan(today.AddDate(0, 0, -1), 300)
	plan(today, 240)
	plan(today.AddDate(0, 0, 3), 120)

	left, err := d.PlannedFromDayForUser(ctx, teamID, proj.ID, ownerID, today)
	if err != nil || left != 360 {
		t.Fatalf("planned from today = %d (err %v), want 360", left, err)
	}
	moved, err := d.MovePlanFromDay(ctx, appmodel.PlanMoveRequest{TeamID: teamID, ProjectID: proj.ID, FromUserID: ownerID, ToUserID: newID, CallerID: ownerID, From: today})
	if err != nil || moved != 360 {
		t.Fatalf("moved %d (err %v), want 360", moved, err)
	}
	if got, _ := d.PlannedFromDayForUser(ctx, teamID, proj.ID, ownerID, today); got != 0 {
		t.Fatalf("the old assignee keeps %d minutes from today on", got)
	}
	if got, _ := d.PlannedFromDayForUser(ctx, teamID, proj.ID, newID, today); got != 360 {
		t.Fatalf("the new assignee has %d minutes from today on, want 360", got)
	}
	var past int
	if err := d.sql.QueryRowContext(ctx, `SELECT minutes FROM schedule_entries WHERE team_id = ? AND user_id = ? AND project_id = ? AND day = ?`,
		teamID, ownerID, proj.ID, "2026-10-21").Scan(&past); err != nil || past != 300 {
		t.Fatalf("yesterday's plan changed to %d (err %v), want 300 left with the old assignee", past, err)
	}

	outsider := requestctx.WithActor(t.Context(), newID+1000)
	if _, err := d.MovePlanFromDay(outsider, appmodel.PlanMoveRequest{TeamID: teamID, ProjectID: proj.ID, FromUserID: newID, ToUserID: ownerID, CallerID: newID + 1000, From: today}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a person without the right to plan moved the plan: err %v", err)
	}
}
