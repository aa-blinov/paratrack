package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A task named like an activity of its project takes that activity over. A name
// whose activity would move someone else's time (another task, another project,
// the archive) is refused with its own reason, and nothing is written (TASK-15).
func TestTaskNameReusesFreeActivityOfItsProjectAndRefusesTheRest(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Названия", "task-names")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	site, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	shop, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Магазин", Color: "#16a34a"})
	if err != nil {
		t.Fatal(err)
	}
	assign := func(activityID, projectID int64) {
		t.Helper()
		if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activityID, ProjectID: projectID, CallerID: ownerID}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(title string, projectID int64) (model.Task, error) {
		t.Helper()
		return d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: projectID, Title: title, Status: model.TaskStatusTodo})
	}
	countNamed := func(name string) int {
		t.Helper()
		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM activities WHERE team_id = ? AND name_key = ?`, teamID, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	call := newTestActivity(t, d, ctx, teamID, ownerID, "Созвон")
	assign(call, site.ID)
	task, err := create("Созвон", site.ID)
	if err != nil {
		t.Fatalf("a task named like a free activity of its project: %v", err)
	}
	var holder int64
	if err := d.sql.QueryRowContext(ctx, `SELECT task_id FROM activities WHERE id = ?`, call).Scan(&holder); err != nil || holder != task.ID {
		t.Fatalf("the activity holds task %d (err %v), want the new task %d", holder, err, task.ID)
	}
	if n := countNamed("созвон"); n != 1 {
		t.Fatalf("activities named Созвон = %d, want the one reused row", n)
	}

	if _, err := create("Созвон", site.ID); !errors.Is(err, appmodel.ErrTaskNameTaken) {
		t.Fatalf("a second task on the same activity: err %v, want ErrTaskNameTaken", err)
	}

	elsewhere := newTestActivity(t, d, ctx, teamID, ownerID, "Обед")
	assign(elsewhere, shop.ID)
	if _, err := create("Обед", site.ID); !errors.Is(err, appmodel.ErrTaskNameTaken) {
		t.Fatalf("an activity of another project: err %v, want ErrTaskNameTaken", err)
	}
	if n := countNamed("обед"); n != 1 {
		t.Fatalf("the refused task changed the activities: %d rows named Обед", n)
	}

	if _, err := d.LinkTaskActivity(ctx, appmodel.TaskActivityRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Name: "Обед"}); !errors.Is(err, appmodel.ErrTaskNameTaken) {
		t.Fatalf("adding a name of another project's activity: err %v, want ErrTaskNameTaken", err)
	}
}
