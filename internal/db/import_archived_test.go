package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/model"
)

// An import writes history the way new time is written: an entry that would land
// on an archived task's activity is skipped and counted, and the rest is kept.
func TestImportSkipsEntriesOnArchivedTaskActivities(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Архив", "archive-import")
	ownerID := teamOwner(t, d, teamID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Вёрстка", Status: model.TaskStatusTodo})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	entries := []importport.ImportedEntry{
		{Activity: "Вёрстка", Start: start, End: start.Add(time.Hour)},
		{Activity: "Ревью", Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)},
	}
	result, err := d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: ownerID, Entries: entries})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 1 || result.ArchivedSkipped != 1 {
		t.Fatalf("import result = %+v, want 1 imported and 1 skipped for the archive", result)
	}
	var onTask int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions s JOIN activities a ON a.id = s.activity_id WHERE a.task_id = ?`, task.ID).Scan(&onTask); err != nil {
		t.Fatalf("count sessions on the task: %v", err)
	}
	if onTask != 0 {
		t.Fatalf("the import wrote %d sessions onto an archived task", onTask)
	}
}
