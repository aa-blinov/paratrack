package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Archiving a task stops the timers running on its activities and keeps the time
// they logged. Until the task is put back on the board, no new time lands on its
// activities: not by a timer, a focus, a backfill or a timesheet cell.
func TestArchivingStopsTimersAndRefusesNewTime(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "archive-time-owner@test.local")
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "archive-time", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Вёрстка", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT id FROM activities WHERE task_id = ?`, task.ID).Scan(&activityID); err != nil {
		t.Fatalf("find the task's activity: %v", err)
	}

	running, err := d.StartSession(ownerCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: time.Now().UTC().Add(-30 * time.Minute)})
	if err != nil {
		t.Fatalf("start a timer: %v", err)
	}
	if err := d.ArchiveTask(ownerCtx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	var end sql.NullString
	var seconds int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT end_at, accumulated_seconds FROM sessions WHERE id = ?`, running.ID).Scan(&end, &seconds); err != nil {
		t.Fatalf("read the timer: %v", err)
	}
	if !end.Valid {
		t.Fatalf("the timer is still running after the archive")
	}
	if seconds < 30*60 || seconds > 31*60 {
		t.Fatalf("the archived timer kept %d seconds, want about 1800", seconds)
	}

	later := time.Now().UTC()
	if _, err := d.StartSession(ownerCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: later}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a timer on an archived task: %v, want ErrTaskArchived", err)
	}
	if _, err := d.FocusActivity(ownerCtx, appmodel.TimerFocusRequest{TeamID: teamID, ActivityID: activityID, At: later}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a focus on an archived task: %v, want ErrTaskArchived", err)
	}
	if _, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: later.Add(-3 * time.Hour), End: later.Add(-2 * time.Hour)}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a backfill on an archived task: %v, want ErrTaskArchived", err)
	}
	if err := d.UpsertDayTotal(ownerCtx, appmodel.TimesheetCellUpdateRequest{TeamID: teamID, ActivityID: activityID, Day: later.AddDate(0, 0, -1), TotalSeconds: 3600}); !errors.Is(err, appmodel.ErrTaskArchived) {
		t.Fatalf("a timesheet cell on an archived task: %v, want ErrTaskArchived", err)
	}

	if err := d.RestoreTask(ownerCtx, appmodel.TaskRestoreRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := d.StartSession(ownerCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: later}); err != nil {
		t.Fatalf("a timer after the restore: %v", err)
	}
}
