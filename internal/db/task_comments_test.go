package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A task's feed keeps what each person wrote, in order, with the author's name.
// An empty or over-long line is refused, and so is a line on an archived task
// (TASK-02).
func TestTaskFeedKeepsLinesInOrderWithTheirAuthors(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Лента", "task-feed")
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
	first, err := d.AddTaskComment(ctx, appmodel.TaskCommentRequest{TeamID: teamID, TaskID: task.ID, CallerID: ownerID, Body: "  Макет согласован.  "})
	if err != nil {
		t.Fatal(err)
	}
	if first.Body != "Макет согласован." || first.Author == "" {
		t.Fatalf("written line = %+v, want the trimmed body and the author's name", first)
	}
	if _, err := d.AddTaskComment(ctx, appmodel.TaskCommentRequest{TeamID: teamID, TaskID: task.ID, CallerID: ownerID, Body: "Готово."}); err != nil {
		t.Fatal(err)
	}
	feed, err := d.ListTaskComments(ctx, teamID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 || feed[0].Body != "Макет согласован." || feed[1].Body != "Готово." {
		t.Fatalf("feed = %+v, want two lines in the order written", feed)
	}
	if _, err := d.AddTaskComment(ctx, appmodel.TaskCommentRequest{TeamID: teamID, TaskID: task.ID, CallerID: ownerID, Body: "   "}); !errors.Is(err, appmodel.ErrInvalidTask) {
		t.Fatalf("an empty line: err %v, want ErrInvalidTask", err)
	}
	if _, err := d.AddTaskComment(ctx, appmodel.TaskCommentRequest{TeamID: teamID, TaskID: task.ID, CallerID: ownerID, Body: strings.Repeat("я", 2001)}); !errors.Is(err, appmodel.ErrInvalidTask) {
		t.Fatalf("an over-long line: err %v, want ErrInvalidTask", err)
	}
	if err := d.ArchiveTask(ctx, appmodel.TaskArchiveRequest{TeamID: teamID, CallerID: ownerID, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddTaskComment(ctx, appmodel.TaskCommentRequest{TeamID: teamID, TaskID: task.ID, CallerID: ownerID, Body: "После архива."}); err == nil {
		t.Fatal("a line was written to an archived task")
	}
}
