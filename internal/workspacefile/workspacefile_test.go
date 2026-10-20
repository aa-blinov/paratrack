package workspacefile

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// stubStore writes one team and one project into the sink it is given, as the
// database layer does, and refuses when refuse is set.
type stubStore struct {
	refuse bool
}

func (s stubStore) ExportWorkspace(_ context.Context, request appmodel.WorkspaceExportRequest) error {
	if s.refuse {
		return model.ErrForbidden
	}
	sink := request.Sink
	if err := sink.Header(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)); err != nil {
		return err
	}
	if err := sink.Team(appmodel.ExportTeam{Slug: "studio", Name: "Студия"}); err != nil {
		return err
	}
	return sink.Project(appmodel.ExportProject{ID: 7, Name: "Сайт", Slug: "site"})
}

func (s stubStore) ResolveWorkspaceEmails(context.Context, []string) (map[string]int64, []string, error) {
	return nil, nil, nil
}

func (s stubStore) RestoreWorkspace(context.Context, appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error) {
	return appmodel.WorkspaceRestoreResult{}, errors.New("not used here")
}

// What is written reads back as the same workspace, and the download learns the
// slug and the export time before its first byte.
func TestExportWritesAFileThatReadsBack(t *testing.T) {
	service, err := New(stubStore{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	var body bytes.Buffer
	var slug string
	var when time.Time
	err = service.Export(t.Context(), appmodel.WorkspaceExportRequest{TeamID: 1, CallerID: 1}, &body,
		func(s string, at time.Time) {
			if body.Len() != 0 {
				t.Fatalf("the headers were asked for after %d bytes were written", body.Len())
			}
			slug, when = s, at
		})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if slug != "studio" || !when.Equal(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("the download learned %q at %v", slug, when)
	}
	back, err := Read(&body)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if back.Format != appmodel.WorkspaceExportFormat || back.Team.Slug != "studio" || len(back.Projects) != 1 || back.Projects[0].Name != "Сайт" {
		t.Fatalf("the file reads back as %+v", back)
	}
}

// A refused export writes nothing and does not start a download.
func TestRefusedExportWritesNothing(t *testing.T) {
	service, err := New(stubStore{refuse: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	var body bytes.Buffer
	called := false
	err = service.Export(t.Context(), appmodel.WorkspaceExportRequest{TeamID: 1, CallerID: 1}, &body,
		func(string, time.Time) { called = true })
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("export: %v, want ErrForbidden", err)
	}
	if called || body.Len() != 0 {
		t.Fatalf("a refused export wrote %d bytes and started a download: %v", body.Len(), called)
	}
}

// A file of another format or another version is refused before any of its
// content reaches a sink, and so is a file that is not JSON at all.
func TestReadRefusesOtherFiles(t *testing.T) {
	for name, content := range map[string]string{
		"another format":  `{"format":"something-else","version":1}`,
		"another version": `{"format":"paratrack-workspace","version":2}`,
		"not json":        `not json`,
		"no header":       `{"team":{"slug":"x"}}`,
		"header late":     `{"team":{"slug":"x"},"format":"paratrack-workspace","version":1}`,
	} {
		if _, err := Read(strings.NewReader(content)); !errors.Is(err, ErrUnsupportedFile) {
			t.Fatalf("%s: %v, want ErrUnsupportedFile", name, err)
		}
	}
}
