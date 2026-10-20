package db

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/workspacefile"
)

// A manager's export holds the whole workspace: people by email, clients,
// projects with their rates, tasks with their activities, finished time with its
// tags, invoices, payroll, the schedule, approvals and plans. It holds no secret,
// and a running timer is not finished work, so it is left out. A member may not
// export: the file carries everyone's rates.
func TestWorkspaceExportHoldsTheWorkButNoSecrets(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "export")

	if _, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member exports the workspace: %v, want forbidden", err)
	}
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	out, err := workspacefile.Read(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("read the file back: %v", err)
	}
	if out.Format != appmodel.WorkspaceExportFormat || out.Version != appmodel.WorkspaceExportVersion || out.Team.Currency != "RUB" || out.Team.RoundMinutes != 15 {
		t.Fatalf("header and team settings are %q v%d %+v", out.Format, out.Version, out.Team)
	}
	if len(out.Members) != 2 || out.Members[0].HourlyPayCents+out.Members[1].HourlyPayCents != 150000 {
		t.Fatalf("members are %+v", out.Members)
	}
	if len(out.Clients) != 1 || out.Clients[0].Name != "Ромашка" || len(out.Projects) != 1 || out.Projects[0].ClientID != ws.clientID || out.Projects[0].BillableRateCents != 500000 || out.Projects[0].LeadEmail != "export-owner@test.local" || out.Projects[0].Deadline != "2026-12-31" {
		t.Fatalf("clients and projects are %+v / %+v", out.Clients, out.Projects)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].Title != "Вёрстка" || out.Tasks[0].AssigneeEmail != "export-member@test.local" || out.Tasks[0].EstimateMinutes != 240 {
		t.Fatalf("tasks are %+v", out.Tasks)
	}
	linked := 0
	for _, a := range out.Activities {
		if a.TaskID == ws.taskID {
			linked++
		}
	}
	if len(out.Activities) != 2 || linked != 2 {
		t.Fatalf("activities are %+v, want the task's two", out.Activities)
	}
	if len(out.Sessions) != 2 {
		t.Fatalf("exported %d sessions, want the two finished ones (the running timer is left out)", len(out.Sessions))
	}
	for _, session := range out.Sessions {
		if session.EndAt == nil {
			t.Fatalf("a running or paused timer was exported: %+v", session)
		}
	}
	if out.Sessions[0].UserEmail != "export-owner@test.local" || len(out.Sessions[0].TagIDs) != 1 || out.Sessions[0].InvoiceID == 0 {
		t.Fatalf("the owner's session is %+v, want its tag and its invoice", out.Sessions[0])
	}
	if out.Sessions[1].UserEmail != "export-member@test.local" {
		t.Fatalf("the member's session carries %q", out.Sessions[1].UserEmail)
	}
	if len(out.Invoices) != 1 || len(out.Invoices[0].Lines) != 1 || out.Invoices[0].Number != "INV-2026-001" || out.Invoices[0].Receipt != "квитанция 17" {
		t.Fatalf("invoices are %+v", out.Invoices)
	}
	if len(out.PayrollRuns) != 1 || len(out.PayrollRuns[0].Lines) != 1 || out.PayrollRuns[0].Lines[0].UserEmail != "export-member@test.local" {
		t.Fatalf("payroll is %+v", out.PayrollRuns)
	}
	if len(out.ScheduleEntries) != 1 || len(out.ScheduleLines) != 1 || len(out.TimesheetApprovals) != 1 || len(out.Goals) != 1 || len(out.SavedReports) != 1 || len(out.Tags) != 1 {
		t.Fatalf("schedule %d/%d, approvals %d, goals %d, reports %d, tags %d", len(out.ScheduleEntries), len(out.ScheduleLines), len(out.TimesheetApprovals), len(out.Goals), len(out.SavedReports), len(out.Tags))
	}
	if len(out.Webhooks) != 1 || len(out.Integrations) != 1 {
		t.Fatalf("webhooks %d, integrations %d", len(out.Webhooks), len(out.Integrations))
	}

	for _, secret := range []string{"HASH_SECRET_TEST", "sk_live_SECRET_TEST", "whsec_SECRET", "gh_SECRET_TOKEN", "SECRET_LINK", "cs_SECRET_SESSION"} {
		if strings.Contains(string(file), secret) {
			t.Fatalf("the export contains a secret: %s", secret)
		}
	}
}
