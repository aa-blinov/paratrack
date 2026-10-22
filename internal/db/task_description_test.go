package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A task carries a description: it is written at creation, changed by an update
// that sends it, and read back on the board. A description longer than the limit
// is refused (TASK-01).
func TestTaskDescriptionIsWrittenUpdatedAndRead(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Описания", "task-descriptions")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Форма",
		Status: model.TaskStatusTodo, Description: "Поля: имя, почта. Готово, когда уходит письмо."})
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		t.Helper()
		var got string
		if err := d.sql.QueryRowContext(ctx, `SELECT description FROM tasks WHERE id = ?`, task.ID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); got != "Поля: имя, почта. Готово, когда уходит письмо." {
		t.Fatalf("description after create = %q", got)
	}
	changed := "Добавили телефон."
	if _, err := d.UpdateTask(ctx, appmodel.TaskUpdateRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Description: &changed}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != changed {
		t.Fatalf("description after update = %q, want %q", got, changed)
	}
	if _, err := d.UpdateTask(ctx, appmodel.TaskUpdateRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Description: nil}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != changed {
		t.Fatalf("an update that does not send the description changed it to %q", got)
	}
	long := strings.Repeat("я", 5001)
	if _, err := d.UpdateTask(ctx, appmodel.TaskUpdateRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID, Description: &long}); !errors.Is(err, appmodel.ErrInvalidTask) {
		t.Fatalf("an over-long description: err %v, want ErrInvalidTask", err)
	}
}
