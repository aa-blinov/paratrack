package db

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/workspacefile"
)

// summarizeExport describes what an export holds, by names and never by ids, so
// two workspaces that hold the same work compare equal though their ids differ.
// Webhooks and integrations are left out: a restore recreates the webhooks with new
// secrets, which the summary does not compare, and brings no integration back.
func summarizeExport(out appmodel.WorkspaceExport) []string {
	projectSlug := map[int64]string{}
	for _, p := range out.Projects {
		projectSlug[p.ID] = p.Slug
	}
	clientName := map[int64]string{}
	for _, c := range out.Clients {
		clientName[c.ID] = c.Name
	}
	activityName := map[int64]string{}
	activityTask := map[int64]string{}
	taskTitle := map[int64]string{}
	for _, t := range out.Tasks {
		taskTitle[t.ID] = t.Title
	}
	for _, a := range out.Activities {
		activityName[a.ID] = a.Name
		if a.TaskID != 0 {
			activityTask[a.ID] = taskTitle[a.TaskID]
		}
	}
	tagName := map[int64]string{}
	for _, tag := range out.Tags {
		tagName[tag.ID] = tag.Name
	}
	invoiceNumber := map[int64]string{}
	for _, inv := range out.Invoices {
		invoiceNumber[inv.ID] = inv.Number
	}

	var lines []string
	for _, m := range out.Members {
		lines = append(lines, "member "+m.Email+" "+m.Role+" rate="+itoa(m.HourlyPayCents)+" cap="+itoa(m.CapacityMinutes)+" admin="+itoa(m.AdminMinutes))
	}
	for _, c := range out.Clients {
		lines = append(lines, "client "+c.Name+" "+c.Email+" "+c.Details+" "+c.Currency)
	}
	for _, p := range out.Projects {
		lines = append(lines, "project "+p.Slug+" "+p.Name+" client="+clientName[p.ClientID]+" rate="+itoa(p.BillableRateCents)+
			" billable="+btoa(p.Billable)+" lead="+p.LeadEmail+" deadline="+p.Deadline+" estimate="+itoa(p.EstimateMinutes))
	}
	for _, a := range out.Activities {
		lines = append(lines, "activity "+a.Name+" project="+projectSlug[a.ProjectID]+" task="+activityTask[a.ID]+" color="+a.Color+" archived="+btoa(a.Archived))
	}
	for _, tag := range out.Tags {
		lines = append(lines, "tag "+tag.Name)
	}
	for _, s := range out.Sessions {
		var tags []string
		for _, id := range s.TagIDs {
			tags = append(tags, tagName[id])
		}
		sort.Strings(tags)
		end := ""
		if s.EndAt != nil {
			end = *s.EndAt
		}
		lines = append(lines, "session "+s.UserEmail+" "+activityName[s.ActivityID]+" "+s.StartAt+" "+end+" note="+s.Note+
			" seconds="+itoa(s.AccumulatedSeconds)+" tags="+strings.Join(tags, ",")+" invoice="+invoiceNumber[s.InvoiceID])
	}
	for _, g := range out.Goals {
		lines = append(lines, "goal "+activityName[g.ActivityID]+" "+g.Period+" "+itoa(g.TargetMinutes))
	}
	for _, task := range out.Tasks {
		var names []string
		for _, a := range out.Activities {
			if a.TaskID == task.ID {
				names = append(names, a.Name)
			}
		}
		sort.Strings(names)
		lines = append(lines, "task "+task.Title+" status="+task.Status+" project="+projectSlug[task.ProjectID]+
			" assignee="+task.AssigneeEmail+" due="+task.DueDate+" estimate="+itoa(int64(task.EstimateMinutes))+
			" archived="+btoa(task.Archived)+" activities="+strings.Join(names, ","))
	}
	for _, inv := range out.Invoices {
		var parts []string
		for _, l := range inv.Lines {
			parts = append(parts, l.Label+"/"+l.Detail+"/"+itoa(l.Seconds)+"/"+itoa(l.AmountCents))
		}
		lines = append(lines, "invoice "+inv.Number+" "+inv.ClientName+" "+inv.Status+" "+inv.Notes+" receipt="+inv.Receipt+
			" paid="+inv.PaidAt+" revision="+itoa(inv.Revision)+" project="+projectSlug[inv.ProjectID]+" lines="+strings.Join(parts, ";"))
	}
	for _, run := range out.PayrollRuns {
		var parts []string
		for _, l := range run.Lines {
			parts = append(parts, l.UserEmail+"/"+l.Label+"/"+itoa(l.AmountCents))
		}
		lines = append(lines, "payroll "+run.Number+" "+run.Status+" "+run.Notes+" lines="+strings.Join(parts, ";"))
	}
	for _, e := range out.ScheduleEntries {
		lines = append(lines, "schedule "+e.UserEmail+" "+projectSlug[e.ProjectID]+" "+e.Day+" "+itoa(e.Minutes)+" "+e.Note)
	}
	for _, l := range out.ScheduleLines {
		lines = append(lines, "schedule-line "+l.UserEmail+" "+projectSlug[l.ProjectID])
	}
	for _, a := range out.TimesheetApprovals {
		lines = append(lines, "approval "+a.StartsAt+" "+a.EndsAt+" by="+a.ApprovedByEmail)
	}
	for _, rep := range out.SavedReports {
		lines = append(lines, "report "+rep.Name+" "+rep.Period+" project="+projectSlug[rep.ProjectID]+" tag="+tagName[rep.TagID]+" slug="+rep.ProjectSlug)
	}
	sort.Strings(lines)
	return lines
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func btoa(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// A workspace exported and restored holds the same work: the same people with the
// same roles and rates, the same clients and projects, tasks linked to their
// activities, finished time with tags and invoices, payroll, the schedule,
// approved weeks and saved reports. The restored workspace is the caller's.
func TestRestoredWorkspaceHoldsWhatWasExported(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "roundtrip")
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	before, err := workspacefile.Read(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}

	restored, err := restoreFile(ctx, d, ws.ownerID, file)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.TeamID == ws.teamID || restored.Slug != "roundtrip-2" {
		t.Fatalf("restored workspace is %+v, want a new one with a free slug", restored)
	}
	again, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: restored.TeamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export the restored workspace: %v", err)
	}
	after, err := workspacefile.Read(bytes.NewReader(again))
	if err != nil {
		t.Fatalf("read the restored export: %v", err)
	}
	if after.Team.Currency != before.Team.Currency || after.Team.RoundMinutes != before.Team.RoundMinutes || after.Team.Name != before.Team.Name ||
		after.Team.Logo != before.Team.Logo || after.Team.Requisites != before.Team.Requisites || before.Team.Logo == "" {
		t.Fatalf("settings changed: before %+v, after %+v", before.Team, after.Team)
	}
	a, b := summarizeExport(before), summarizeExport(after)
	for _, kind := range []string{"member ", "project ", "activity ", "session ", "task ", "invoice ", "payroll ", "schedule ", "approval ", "report ", "goal "} {
		if !hasLinePrefix(a, kind) {
			t.Fatalf("the summary holds no %q line, so the comparison would prove nothing", kind)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the restored workspace differs:\nbefore:\n  %s\nafter:\n  %s", strings.Join(a, "\n  "), strings.Join(b, "\n  "))
	}
}

// A restore recreates each webhook of the file with a new secret. The owner sees
// that secret once, in the result; the file never held the old one, and the
// stored secret is the one returned.
func TestRestoreRecreatesWebhooksWithNewSecrets(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "hooks")
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restored, err := restoreFile(ctx, d, ws.ownerID, file)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(restored.Webhooks) != 1 {
		t.Fatalf("restored %d webhooks, want the one the workspace had", len(restored.Webhooks))
	}
	hook := restored.Webhooks[0]
	if hook.URL == "" || len(hook.Secret) < 32 || strings.Contains(string(file), hook.Secret) {
		t.Fatalf("the restored webhook %+v has no usable new secret", hook)
	}
	var url, sealed string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT url, secret FROM webhooks WHERE team_id = ?`, restored.TeamID).Scan(&url, &sealed); err != nil {
		t.Fatalf("read the restored webhook: %v", err)
	}
	stored, err := d.openSecret(sealed)
	if err != nil {
		t.Fatalf("open the stored secret: %v", err)
	}
	if url != hook.URL || stored != hook.Secret {
		t.Fatalf("stored webhook %q with secret %q, want %q with the returned secret", url, stored, hook.URL)
	}
}

// A file that names someone this installation does not know is refused before any
// row is written, and the refusal names every such person.
func TestRestoreRefusesUnknownPeopleWithoutWriting(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "unknown-people")
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	file = bytes.ReplaceAll(file, []byte("unknown-people-member@test.local"), []byte("ghost@nowhere.test"))
	file = bytes.ReplaceAll(file, []byte("unknown-people-owner@test.local"), []byte("phantom@nowhere.test"))
	workspacesBefore := workspacesOwnedBy(ctx, t, d, ws.ownerID)

	_, err = restoreFile(ctx, d, ws.ownerID, file)
	if !errors.Is(err, appmodel.ErrRestorePeopleMissing) {
		t.Fatalf("restore with unknown people: %v, want ErrRestorePeopleMissing", err)
	}
	for _, email := range []string{"ghost@nowhere.test", "phantom@nowhere.test"} {
		if !strings.Contains(err.Error(), email) {
			t.Fatalf("the refusal %q does not name %s", err.Error(), email)
		}
	}
	if workspacesOwnedBy(ctx, t, d, ws.ownerID) != workspacesBefore {
		t.Fatalf("a refused restore left a workspace behind")
	}
}

// A file cut short, as a dropped upload leaves it, is refused and nothing is kept.
func TestRestoreRefusesACutOffFileWithoutWriting(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "cut-off")
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	workspacesBefore := workspacesOwnedBy(ctx, t, d, ws.ownerID)

	if _, err := restoreFile(ctx, d, ws.ownerID, file[:len(file)/2]); !errors.Is(err, workspacefile.ErrUnsupportedFile) {
		t.Fatalf("restore of a cut-off file: %v, want ErrUnsupportedFile", err)
	}
	if workspacesOwnedBy(ctx, t, d, ws.ownerID) != workspacesBefore {
		t.Fatalf("a cut-off restore left a workspace behind")
	}
}

// exportFile writes the workspace the way a download does, and returns the file.
func exportFile(ctx context.Context, d *DB, request appmodel.WorkspaceExportRequest) ([]byte, error) {
	svc, err := workspacefile.New(d)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := svc.Export(ctx, request, &buf, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// restoreFile restores a file into a new workspace of the caller.
func restoreFile(ctx context.Context, d *DB, callerID int64, file []byte) (appmodel.WorkspaceRestoreResult, error) {
	svc, err := workspacefile.New(d)
	if err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	return svc.Restore(ctx, callerID, bytes.NewReader(file))
}

// workspacesOwnedBy counts the workspaces a person owns, to show a refused restore
// left none behind.
func workspacesOwnedBy(ctx context.Context, t *testing.T, d *DB, ownerID int64) int {
	t.Helper()
	var n int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM teams WHERE owner_id = ?`, ownerID).Scan(&n); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	return n
}

func hasLinePrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
