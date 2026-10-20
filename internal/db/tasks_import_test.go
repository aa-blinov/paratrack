package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A GitHub board imports once and then follows: items become tasks with their
// column, a second run updates them instead of copying them, an archived task is
// left alone, and only a manager may import.
func TestGitHubBoardImportCreatesUpdatesAndKeepsArchivedTasks(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Доска", "github-board")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "github-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	board := []appmodel.TaskImportItem{
		{Ref: "https://github.com/acme/site/issues/12", Title: "acme/site#12 Fix login", URL: "https://github.com/acme/site/issues/12", Status: model.TaskStatusTodo},
		{Ref: "github-draft:PVTI_2", Title: "Think about pricing", Status: model.TaskStatusDoing},
	}
	importBoard := func(caller int64, items []appmodel.TaskImportItem) (appmodel.TaskImportResult, error) {
		return d.ImportTasks(ctx, appmodel.TaskImportRequest{TeamID: teamID, CallerID: caller, ProjectID: proj.ID, Items: items})
	}

	if result, err := importBoard(memberID, board); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member imports a board: result %+v, error %v, want forbidden", result, err)
	}
	if result, err := importBoard(ownerID, board); err != nil || result.Created != 2 {
		t.Fatalf("first import: %+v, %v, want 2 created", result, err)
	}
	if result, err := importBoard(ownerID, board); err != nil || result.Unchanged != 2 || result.Created != 0 {
		t.Fatalf("the same board again: %+v, %v, want 2 unchanged and nothing copied", result, err)
	}

	moved := []appmodel.TaskImportItem{
		{Ref: "https://github.com/acme/site/issues/12", Title: "acme/site#12 Fix login (merged)", URL: "https://github.com/acme/site/issues/12", Status: model.TaskStatusDone},
		{Ref: "github-draft:PVTI_2", Title: "Think about pricing", Status: model.TaskStatusDoing},
	}
	if result, err := importBoard(ownerID, moved); err != nil || result.Updated != 1 || result.Unchanged != 1 {
		t.Fatalf("the board moved one item: %+v, %v, want 1 updated and 1 unchanged", result, err)
	}
	rows, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: time.Now().UTC()})
	if err != nil || len(rows) != 2 {
		t.Fatalf("the workspace has %d open tasks (%v), want the 2 imported ones and no copies", len(rows), err)
	}
	var done appmodel.TaskRow
	for _, row := range rows {
		if row.Task.ExternalURL != "" {
			done = row
		}
	}
	if done.Task.Status != model.TaskStatusDone || done.Task.Title != "acme/site#12 Fix login (merged)" || done.Task.ExternalURL != "https://github.com/acme/site/issues/12" {
		t.Fatalf("the moved task is %+v, want done, renamed, and linked to the issue", done.Task)
	}

	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: ownerID, TaskID: done.Task.ID}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	changed := []appmodel.TaskImportItem{{Ref: "https://github.com/acme/site/issues/12", Title: "renamed on GitHub", URL: "https://github.com/acme/site/issues/12", Status: model.TaskStatusTodo}}
	if result, err := importBoard(ownerID, changed); err != nil || result.Archived != 1 || result.Created != 0 {
		t.Fatalf("an archived item comes back: %+v, %v, want counted as archived and left alone", result, err)
	}
	archive, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Archived: true, Now: time.Now().UTC()})
	if err != nil || len(archive) != 1 || archive[0].Task.Title != "acme/site#12 Fix login (merged)" {
		t.Fatalf("the archived task changed: %+v (%v)", archive, err)
	}
}
