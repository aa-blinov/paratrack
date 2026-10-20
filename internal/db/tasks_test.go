package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A task starts with its own activity, takes on more activities, adds up the
// time logged to all of them, and can be taken off the board without losing it.
func TestTaskHoldsActivitiesAndKeepsTheirTime(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "task-owner@test.local")
	memberID := seedApprovalUser(t, d, "task-member@test.local")
	outsiderID := seedApprovalUser(t, d, "task-outsider@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "task-team", Name: "Команда", OwnerID: ownerID})
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

	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: memberID, ProjectID: proj.ID, Title: "Вёрстка главной", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatalf("a member creates a task: %v", err)
	}
	rows, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Activities) != 1 || rows[0].Activities[0].Name != "Вёрстка главной" {
		t.Fatalf("a new task has no activity of its own: %+v", rows)
	}

	review, err := d.LinkTaskActivity(ctx, appmodel.TaskActivityRequest{TeamID: teamID, CallerID: memberID, TaskID: task.ID, Name: "Ревью"})
	if err != nil {
		t.Fatalf("add a second activity: %v", err)
	}
	seedClosedSession(t, d, teamID, memberID, review.ID, time.Date(2020, 3, 2, 10, 0, 0, 0, time.UTC))
	rows, _ = d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: time.Now().UTC()})
	if len(rows[0].Activities) != 2 {
		t.Fatalf("the task holds %d activities, want 2", len(rows[0].Activities))
	}
	if rows[0].TrackedMin != 60 {
		t.Fatalf("the task tracked %d minutes, want 60 from its second activity", rows[0].TrackedMin)
	}

	if _, err := d.LinkTaskActivity(ctx, appmodel.TaskActivityRequest{TeamID: teamID, CallerID: memberID, TaskID: task.ID + 999, Name: "Чужая"}); err == nil {
		t.Fatal("an activity was linked to a task that does not exist")
	}
	other, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Другая", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := d.LinkTaskActivity(ctx, appmodel.TaskActivityRequest{TeamID: teamID, CallerID: memberID, TaskID: other.ID, ActivityID: review.ID}); !errors.Is(err, appmodel.ErrTaskActivityTaken) {
		t.Fatalf("an activity held by one task was taken by another = %v, want ErrTaskActivityTaken", err)
	}

	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: outsiderID, TaskID: task.ID}); err == nil {
		t.Fatal("a plain member who did not create the task archived it")
	}
	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: memberID, TaskID: task.ID}); err != nil {
		t.Fatalf("the creator archives the task: %v", err)
	}
	rows, _ = d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: time.Now().UTC()})
	for _, row := range rows {
		if row.Task.ID == task.ID {
			t.Fatal("an archived task is still on the board")
		}
	}
	var tracked int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE activity_id = ?`, review.ID).Scan(&tracked); err != nil || tracked != 1 {
		t.Fatalf("archiving lost the task's time: %d sessions left, err %v", tracked, err)
	}
}
