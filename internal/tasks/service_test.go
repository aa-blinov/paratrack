package tasks

import (
	"context"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A closed item is done whatever its column says; an open one follows its Status
// name in English or Russian; no Status or an unknown name starts in «к работе».
func TestColumnForGitHubItem(t *testing.T) {
	cases := []struct {
		status string
		closed bool
		want   string
	}{
		{"Anything", true, model.TaskStatusDone},
		{"", false, model.TaskStatusTodo},
		{"Todo", false, model.TaskStatusTodo},
		{"In Progress", false, model.TaskStatusDoing},
		{"В работе", false, model.TaskStatusDoing},
		{"Review", false, model.TaskStatusDoing},
		{"Done", false, model.TaskStatusDone},
		{"Готово", false, model.TaskStatusDone},
		{"Backlog ideas", false, model.TaskStatusTodo},
	}
	for _, c := range cases {
		if got := ColumnForGitHubItem(c.status, c.closed); got != c.want {
			t.Fatalf("ColumnForGitHubItem(%q, %v) = %q, want %q", c.status, c.closed, got, c.want)
		}
	}
}

type fakeSource struct {
	calls int
}

func (f *fakeSource) FetchGitHubProject(context.Context, string, string) ([]appmodel.GitHubProjectItem, error) {
	f.calls++
	return nil, nil
}

// A request without a token, a board link or a valid workspace is refused before
// the board is read, so a bad form never reaches GitHub.
func TestImportRefusesBadInputBeforeReadingTheBoard(t *testing.T) {
	source := &fakeSource{}
	service, err := New(nil, source, nil)
	if err == nil {
		t.Fatal("a service without a store must not be built")
	}
	service, err = New(&stubStore{}, source, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	bad := []appmodel.GitHubImportRequest{
		{TeamID: 0, CallerID: 1, ProjectID: 1, ProjectURL: "https://github.com/orgs/a/projects/1", Token: "t"},
		{TeamID: 1, CallerID: 1, ProjectID: 1, ProjectURL: "https://github.com/orgs/a/projects/1", Token: "   "},
		{TeamID: 1, CallerID: 1, ProjectID: 1, ProjectURL: "  ", Token: "t"},
		{TeamID: 1, CallerID: 1, ProjectID: 0, ProjectURL: "https://github.com/orgs/a/projects/1", Token: "t"},
	}
	for i, request := range bad {
		if _, err := service.ImportGitHubProject(t.Context(), request); err != appmodel.ErrInvalidTask {
			t.Fatalf("request %d: %v, want ErrInvalidTask", i, err)
		}
	}
	if source.calls != 0 {
		t.Fatalf("invalid requests reached the board reader %d times", source.calls)
	}
}

// stubStore satisfies Store for the validation tests, which never reach it.
type stubStore struct{ Store }
