package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Starting time on a task moves a task that is "to do" into "in progress", at the
// end of that column. Other tasks keep their status (TIME-04).
func TestStartingWorkMovesOnlyATodoTaskIntoProgress(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Старт", "task-start")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(title string) model.Task {
		t.Helper()
		task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: title, Status: model.TaskStatusTodo})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	status := func(id int64) string {
		t.Helper()
		var got string
		if err := d.sql.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	todo := create("Вёрстка")
	doing := create("Макет")
	if err := d.SetTaskStatus(ctx, appmodel.TaskStatusRequest{TeamID: teamID, CallerID: ownerID, TaskID: doing.ID, Status: model.TaskStatusDoing}); err != nil {
		t.Fatal(err)
	}

	activity := newTestActivity(t, d, ctx, teamID, ownerID, "Обсуждение")
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity, ProjectID: proj.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if got, err := d.TaskOfActivity(ctx, teamID, activity); err != nil || got != 0 {
		t.Fatalf("a plain activity belongs to task %d (err %v), want none", got, err)
	}
	var linked int64
	if err := d.sql.QueryRowContext(ctx, `SELECT id FROM activities WHERE team_id = ? AND task_id = ?`, teamID, todo.ID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if got, err := d.TaskOfActivity(ctx, teamID, linked); err != nil || got != todo.ID {
		t.Fatalf("the task's activity names task %d (err %v), want %d", got, err, todo.ID)
	}

	if err := d.StartTaskWork(ctx, appmodel.TaskStartRequest{TeamID: teamID, TaskID: todo.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if got := status(todo.ID); got != model.TaskStatusDoing {
		t.Fatalf("status after work started = %q, want doing", got)
	}
	if err := d.StartTaskWork(ctx, appmodel.TaskStartRequest{TeamID: teamID, TaskID: doing.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if got := status(doing.ID); got != model.TaskStatusDoing {
		t.Fatalf("a task already under way changed to %q", got)
	}
	var position int
	if err := d.sql.QueryRowContext(ctx, `SELECT position FROM tasks WHERE id = ?`, todo.ID).Scan(&position); err != nil {
		t.Fatal(err)
	}
	if position < 2 {
		t.Fatalf("the started task kept position %d, want the end of the doing column", position)
	}
}
