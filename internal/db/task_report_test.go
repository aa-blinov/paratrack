package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The task report counts the time a task got inside its period (TASK-04): a closed
// session for the part inside it, an open timer up to now, and an archived task
// keeps its time. Time on an activity that no task holds counts for no task.
func TestTaskReportCountsTimeInsideThePeriod(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Отчёт", "task-report")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Вёрстка", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatal(err)
	}
	var taskActivity int64
	if err := d.sql.QueryRowContext(ctx, `SELECT id FROM activities WHERE team_id = ? AND task_id = ?`, teamID, task.ID).Scan(&taskActivity); err != nil {
		t.Fatal(err)
	}
	loose := newTestActivity(t, d, ctx, teamID, ownerID, "Без задачи")

	day := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, taskActivity, day.Add(9*time.Hour))                    // 09:00-10:00, inside
	seedClosedSession(t, d, teamID, ownerID, taskActivity, day.AddDate(0, 0, -1).Add(10*time.Hour)) // the day before, outside
	seedClosedSession(t, d, teamID, ownerID, loose, day.Add(11*time.Hour))                          // no task, inside
	midnight := day.Add(-30 * time.Minute)                                                          // 23:30 the day before to 00:30, half of it inside
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds)
		 VALUES (?, ?, ?, ?, ?, 3600)`,
		taskActivity, teamID, ownerID, FormatTime(midnight), FormatTime(midnight.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	running := day.Add(15 * time.Hour)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds, paused, last_resume_at)
		 VALUES (?, ?, ?, ?, NULL, 0, 0, ?)`,
		taskActivity, teamID, ownerID, FormatTime(running), FormatTime(running)); err != nil {
		t.Fatal(err)
	}

	query := appmodel.TaskReportQuery{TeamID: teamID, Start: day, End: day.AddDate(0, 0, 1), Now: day.Add(16 * time.Hour)}
	seconds, err := d.TrackedSecondsByTask(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	// 09:00-10:00 is an hour, the midnight session leaves half an hour inside the
	// day, and the timer that runs from 15:00 to now (16:00) is an hour more.
	if got := seconds[task.ID]; got != 3600+1800+3600 {
		t.Fatalf("task time in the period = %d s, want %d", got, 3600+1800+3600)
	}
	if len(seconds) != 1 {
		t.Fatalf("the report holds %d tasks, want only the one that was tracked", len(seconds))
	}

	// Archiving stops the timers still running on the task, which would spread the
	// running one over the days since it started, so the archived task has its own
	// closed hour.
	archived, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Архив", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatal(err)
	}
	var archivedActivity int64
	if err := d.sql.QueryRowContext(ctx, `SELECT id FROM activities WHERE team_id = ? AND task_id = ?`, teamID, archived.ID).Scan(&archivedActivity); err != nil {
		t.Fatal(err)
	}
	seedClosedSession(t, d, teamID, ownerID, archivedActivity, day.Add(12*time.Hour))
	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: ownerID, TaskID: archived.ID}); err != nil {
		t.Fatal(err)
	}
	seconds, err = d.TrackedSecondsByTask(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if got := seconds[archived.ID]; got != 3600 {
		t.Fatalf("an archived task lost its time: %d s, want 3600", got)
	}

	if _, err := d.TrackedSecondsByTask(ctx, appmodel.TaskReportQuery{TeamID: teamID, Start: day, End: day}); err == nil {
		t.Fatal("an empty period was accepted")
	}
}
