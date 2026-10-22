package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Only open, assigned tasks due by the given day are candidates for a due-date
// reminder, and the marker a reminder leaves is read back (TASK-05).
func TestTasksDueOnPicksOpenAssignedTasksAndKeepsTheMarker(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Сроки", "due-reminders")
	ownerID := teamOwner(t, d, teamID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	create := func(title string, status string, assignee int64, due string) int64 {
		t.Helper()
		task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{
			TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: title, Status: status,
			AssigneeID: assignee, Due: due,
		})
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return task.ID
	}
	due := create("Бриф", model.TaskStatusTodo, ownerID, "2026-11-03")
	create("Готово", model.TaskStatusDone, ownerID, "2026-11-03")
	create("Без исполнителя", model.TaskStatusTodo, 0, "2026-11-03")
	create("Позже", model.TaskStatusTodo, ownerID, "2026-12-01")

	candidates, err := d.TasksDueOn(ctx, "2026-11-03")
	if err != nil {
		t.Fatalf("tasks due: %v", err)
	}
	if len(candidates) != 1 || candidates[0].TaskID != due {
		t.Fatalf("candidates = %+v, want only the brief", candidates)
	}
	if candidates[0].Reminded != "" {
		t.Fatalf("a task with no reminder reads marker %q", candidates[0].Reminded)
	}

	if err := d.MarkTaskDueReminded(ctx, appmodel.TaskDueReminderRequest{TeamID: teamID, TaskID: due, Marker: "soon:2026-11-03"}); err != nil {
		t.Fatalf("mark reminded: %v", err)
	}
	again, err := d.TasksDueOn(ctx, "2026-11-03")
	if err != nil {
		t.Fatalf("tasks due again: %v", err)
	}
	if len(again) != 1 || again[0].Reminded != "soon:2026-11-03" {
		t.Fatalf("after the mark = %+v, want the marker kept", again)
	}
}
