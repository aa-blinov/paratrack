package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/workspacefile"
)

// downloadSink keeps the people and the projects of a downloaded file and counts
// nothing else: enough to show the file carries the workspace it was asked for.
type downloadSink struct {
	members  int
	projects []string
}

func (s *downloadSink) Header(time.Time) error               { return nil }
func (s *downloadSink) Team(appmodel.ExportTeam) error       { return nil }
func (s *downloadSink) Member(appmodel.ExportMember) error   { s.members++; return nil }
func (s *downloadSink) Holiday(appmodel.ExportHoliday) error { return nil }
func (s *downloadSink) Absence(appmodel.ExportAbsence) error { return nil }
func (s *downloadSink) Client(appmodel.ExportClient) error   { return nil }
func (s *downloadSink) Project(p appmodel.ExportProject) error {
	s.projects = append(s.projects, p.Name)
	return nil
}
func (s *downloadSink) Tag(appmodel.ExportTag) error                 { return nil }
func (s *downloadSink) Task(appmodel.ExportTask) error               { return nil }
func (s *downloadSink) Activity(appmodel.ExportActivity) error       { return nil }
func (s *downloadSink) Goal(appmodel.ExportGoal) error               { return nil }
func (s *downloadSink) Invoice(appmodel.ExportInvoice) error         { return nil }
func (s *downloadSink) PayrollRun(appmodel.ExportPayrollRun) error   { return nil }
func (s *downloadSink) Session(appmodel.ExportSession) error         { return nil }
func (s *downloadSink) Schedule(appmodel.ExportSchedule) error       { return nil }
func (s *downloadSink) ScheduleRow(appmodel.ExportScheduleRow) error { return nil }
func (s *downloadSink) Approval(appmodel.ExportApproval) error       { return nil }
func (s *downloadSink) Report(appmodel.ExportReport) error           { return nil }
func (s *downloadSink) Webhook(appmodel.ExportWebhook) error         { return nil }
func (s *downloadSink) Integration(appmodel.ExportIntegration) error { return nil }

// An owner downloads the whole workspace as one named JSON file, and the file
// reads back in the export format. It names no password.
func TestWorkspaceExportDownloadsOneFileForAManager(t *testing.T) {
	e := newAPIEnv(t)
	e.register("workspace-export@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Export work"}}, nil))
	page := reactData[exportPage](t, readBody(t, e.do("GET", "/export", nil, nil)))
	if !page.CanExportWorkspace {
		t.Fatal("the owner is not offered the whole-workspace download")
	}

	resp := e.do("GET", "/export/workspace.json", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("download: status %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `attachment; filename="paratrack-`) {
		t.Fatalf("headers: %q %q", resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	var download downloadSink
	if err := workspacefile.Decode(strings.NewReader(body), &download); err != nil {
		t.Fatalf("the download does not read back: %v", err)
	}
	if len(download.projects) != 1 || download.projects[0] != "Export work" || download.members != 1 {
		t.Fatalf("the download holds projects %+v and %d members", download.projects, download.members)
	}
	if strings.Contains(body, "password") {
		t.Fatal("the download names a password field")
	}
}
