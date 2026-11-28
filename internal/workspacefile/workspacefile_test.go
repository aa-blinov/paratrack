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

// memorySink keeps a whole export in memory, so a test can compare a file with
// the workspace it was written from.
type memorySink struct {
	export appmodel.WorkspaceExport
}

func (s *memorySink) Header(at time.Time) error {
	s.export.Format = appmodel.WorkspaceExportFormat
	s.export.Version = appmodel.WorkspaceExportVersion
	s.export.ExportedAt = at
	return nil
}
func (s *memorySink) Team(v appmodel.ExportTeam) error { s.export.Team = v; return nil }
func (s *memorySink) Member(v appmodel.ExportMember) error {
	s.export.Members = append(s.export.Members, v)
	return nil
}
func (s *memorySink) Holiday(v appmodel.ExportHoliday) error {
	s.export.Holidays = append(s.export.Holidays, v)
	return nil
}
func (s *memorySink) Absence(v appmodel.ExportAbsence) error {
	s.export.Absences = append(s.export.Absences, v)
	return nil
}
func (s *memorySink) Client(v appmodel.ExportClient) error {
	s.export.Clients = append(s.export.Clients, v)
	return nil
}
func (s *memorySink) Project(v appmodel.ExportProject) error {
	s.export.Projects = append(s.export.Projects, v)
	return nil
}
func (s *memorySink) Tag(v appmodel.ExportTag) error {
	s.export.Tags = append(s.export.Tags, v)
	return nil
}
func (s *memorySink) Task(v appmodel.ExportTask) error {
	s.export.Tasks = append(s.export.Tasks, v)
	return nil
}
func (s *memorySink) Activity(v appmodel.ExportActivity) error {
	s.export.Activities = append(s.export.Activities, v)
	return nil
}
func (s *memorySink) Goal(v appmodel.ExportGoal) error {
	s.export.Goals = append(s.export.Goals, v)
	return nil
}
func (s *memorySink) Invoice(v appmodel.ExportInvoice) error {
	s.export.Invoices = append(s.export.Invoices, v)
	return nil
}
func (s *memorySink) PayrollRun(v appmodel.ExportPayrollRun) error {
	s.export.PayrollRuns = append(s.export.PayrollRuns, v)
	return nil
}
func (s *memorySink) Session(v appmodel.ExportSession) error {
	s.export.Sessions = append(s.export.Sessions, v)
	return nil
}
func (s *memorySink) Schedule(v appmodel.ExportSchedule) error {
	s.export.ScheduleEntries = append(s.export.ScheduleEntries, v)
	return nil
}
func (s *memorySink) ScheduleRow(v appmodel.ExportScheduleRow) error {
	s.export.ScheduleLines = append(s.export.ScheduleLines, v)
	return nil
}
func (s *memorySink) Approval(v appmodel.ExportApproval) error {
	s.export.TimesheetApprovals = append(s.export.TimesheetApprovals, v)
	return nil
}
func (s *memorySink) Report(v appmodel.ExportReport) error {
	s.export.SavedReports = append(s.export.SavedReports, v)
	return nil
}
func (s *memorySink) Webhook(v appmodel.ExportWebhook) error {
	s.export.Webhooks = append(s.export.Webhooks, v)
	return nil
}
func (s *memorySink) Integration(v appmodel.ExportIntegration) error {
	s.export.Integrations = append(s.export.Integrations, v)
	return nil
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
	var back memorySink
	if err := Decode(&body, &back); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if back.export.Format != appmodel.WorkspaceExportFormat || back.export.Team.Slug != "studio" || len(back.export.Projects) != 1 || back.export.Projects[0].Name != "Сайт" {
		t.Fatalf("the file reads back as %+v", back.export)
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
		var sink memorySink
		if err := Decode(strings.NewReader(content), &sink); !errors.Is(err, ErrUnsupportedFile) {
			t.Fatalf("%s: %v, want ErrUnsupportedFile", name, err)
		}
		if len(sink.export.Projects) != 0 || sink.export.Team.Slug != "" {
			t.Fatalf("%s: the file reached the sink before it was refused", name)
		}
	}
}
