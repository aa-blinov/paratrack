package workspacefile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type stubStore struct {
	export appmodel.WorkspaceExport
	err    error
}

func (s stubStore) ExportWorkspace(context.Context, appmodel.WorkspaceExportRequest) (appmodel.WorkspaceExport, error) {
	return s.export, s.err
}

func (s stubStore) RestoreWorkspace(context.Context, appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error) {
	return appmodel.WorkspaceRestoreResult{}, nil
}

// What is written reads back as the same workspace, and a file of another format
// or another version is refused before its content is used.
func TestExportWritesAFileThatReadsBackAndRefusesOthers(t *testing.T) {
	when := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	service, err := New(stubStore{export: appmodel.WorkspaceExport{
		Format: appmodel.WorkspaceExportFormat, Version: appmodel.WorkspaceExportVersion, ExportedAt: when,
		Team:     appmodel.ExportTeam{Slug: "studio", Name: "Студия"},
		Projects: []appmodel.ExportProject{{ID: 7, Name: "Сайт", Slug: "site"}},
	}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	export, body, err := service.Export(t.Context(), appmodel.WorkspaceExportRequest{TeamID: 1, CallerID: 1})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	back, err := Read(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if back.Team.Slug != "studio" || len(back.Projects) != 1 || back.Projects[0].Name != "Сайт" || back.Projects[0].ID != export.Projects[0].ID {
		t.Fatalf("the file reads back as %+v", back)
	}

	if _, err := Read(strings.NewReader(`{"format":"something-else","version":1}`)); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("another format: %v, want ErrUnsupportedFile", err)
	}
	if _, err := Read(strings.NewReader(`{"format":"paratrack-workspace","version":2}`)); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("another version: %v, want ErrUnsupportedFile", err)
	}
	if _, err := Read(strings.NewReader(`not json`)); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("not json: %v, want ErrUnsupportedFile", err)
	}
}
