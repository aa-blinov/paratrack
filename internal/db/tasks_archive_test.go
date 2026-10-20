package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// An archived task is read-only: edits, status moves and new activities are
// refused as archived, it leaves the open board and turns up in the archive.
// Putting it back restores the edits, and only a manager or the creator may do it.
func TestArchivedTaskIsReadOnlyUntilRestored(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "archive-owner@test.local")
	creatorID := seedApprovalUser(t, d, "archive-creator@test.local")
	otherID := seedApprovalUser(t, d, "archive-other@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "archive-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for _, id := range []int64{creatorID, otherID} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
			teamID, id, FormatTime(time.Now().UTC())); err != nil {
			t.Fatalf("join member: %v", err)
		}
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: creatorID, ProjectID: proj.ID, Title: "Вёрстка", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: creatorID, TaskID: task.ID}); err != nil {
		t.Fatalf("the creator archives: %v", err)
	}

	title := "Другое"
	if _, err := d.UpdateTask(ctx, appmodel.TaskUpdateRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Title: &title}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("an edit to an archived task: %v, want ErrTaskArchived", err)
	}
	if err := d.SetTaskStatus(ctx, appmodel.TaskStatusRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Status: model.TaskStatusDone}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a status move on an archived task: %v, want ErrTaskArchived", err)
	}
	if _, err := d.LinkTaskActivity(ctx, appmodel.TaskActivityRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Name: "Ревью"}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a new activity on an archived task: %v, want ErrTaskArchived", err)
	}

	now := time.Now().UTC()
	open, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: now})
	if err != nil || len(open) != 0 {
		t.Fatalf("the open board lists %d tasks (%v), want none", len(open), err)
	}
	archive, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Archived: true, Now: now})
	if err != nil || len(archive) != 1 || archive[0].Task.ID != task.ID || len(archive[0].Activities) != 1 {
		t.Fatalf("the archive lists %+v (%v), want the one task with its activity", archive, err)
	}
	single, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, TaskID: task.ID, Now: now})
	if err != nil || len(single) != 1 || !single[0].Task.Archived {
		t.Fatalf("the card query returns %+v (%v), want the archived task", single, err)
	}

	if err := d.RestoreTask(ctx, appmodel.TaskRestoreRequest{TeamID: teamID, CallerID: otherID, TaskID: task.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("another member restores the task: %v, want forbidden", err)
	}
	if err := d.RestoreTask(ctx, appmodel.TaskRestoreRequest{TeamID: teamID, CallerID: creatorID, TaskID: task.ID}); err != nil {
		t.Fatalf("the creator restores the task: %v", err)
	}
	if open, err = d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: now}); err != nil || len(open) != 1 {
		t.Fatalf("after the restore the board lists %d tasks (%v), want 1", len(open), err)
	}
	if archive, err = d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Archived: true, Now: now}); err != nil || len(archive) != 0 {
		t.Fatalf("after the restore the archive lists %d tasks (%v), want none", len(archive), err)
	}
	if _, err := d.UpdateTask(ctx, appmodel.TaskUpdateRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Title: &title}); err != nil {
		t.Fatalf("an edit after the restore: %v", err)
	}
}
