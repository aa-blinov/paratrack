package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestBuildInvoiceLinesRejectsMissingWorkspaceScope(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var d *DB
	_, err := d.BuildInvoiceLinesFor(t.Context(), 0, start, start.AddDate(0, 0, 1), 0, 0)
	if !errors.Is(err, ErrInvalidInvoiceQuery) {
		t.Fatalf("BuildInvoiceLinesFor error = %v, want invalid query", err)
	}
}

func TestOverlappingInvoicesRejectsInvalidScope(t *testing.T) {
	var d *DB
	if _, err := d.OverlappingInvoices(t.Context(), appmodel.InvoiceOverlapQuery{}); !errors.Is(err, ErrInvalidInvoiceQuery) {
		t.Fatalf("OverlappingInvoices without scope error = %v, want invalid query", err)
	}
}

func TestOverlappingInvoicesRespectsWorkspacePeriodAndLabels(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "Overlap A", "overlap-a")
	teamB := seedTeam(t, d, "Overlap B", "overlap-b")
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	insertInvoice := func(teamID int64, number string, periodStart, periodEnd time.Time, label string) int64 {
		t.Helper()
		var invoiceID int64
		err := d.TestSQL().QueryRowContext(ctx, `
			INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
			VALUES (?, ?, '', ?, ?, 'draft', ?) RETURNING id`,
			teamID, number, FormatTime(periodStart), FormatTime(periodEnd), FormatTime(start)).Scan(&invoiceID)
		if err != nil {
			t.Fatalf("insert invoice %s: %v", number, err)
		}
		if label != "" {
			if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO invoice_lines (invoice_id, label) VALUES (?, ?)`, invoiceID, label); err != nil {
				t.Fatalf("insert invoice line %s: %v", number, err)
			}
		}
		return invoiceID
	}
	excludedID := insertInvoice(teamA, "INV-CURRENT", start, end, "Design")
	insertInvoice(teamA, "INV-OVERLAP", start.Add(-time.Hour), end.Add(time.Hour), "Design")
	insertInvoice(teamA, "INV-DIFFERENT-LABEL", start, end, "Engineering")
	insertInvoice(teamA, "INV-OUTSIDE-PERIOD", end, end.AddDate(0, 0, 1), "Design")
	insertInvoice(teamB, "INV-OTHER-TEAM", start, end, "Design")

	got, err := d.OverlappingInvoices(ctx, appmodel.InvoiceOverlapQuery{
		TeamID: teamA, ExcludeInvoiceID: excludedID, Start: start, End: end, Labels: []string{"Design"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "INV-OVERLAP" {
		t.Fatalf("overlapping invoices = %v, want [INV-OVERLAP]", got)
	}
}

func TestBuildInvoiceLinesRejectsMalformedSessionTime(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Malformed times", "malformed-times")
	ownerID := teamOwner(t, d, teamID)
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Client", Slug: "client", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	rate := 1000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: teamID, ProjectID: project.ID, CallerID: ownerID, RateCents: &rate}); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: project.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE sessions SET start_at = ? WHERE team_id = ?`, "2026-09-01T99:00:00Z", teamID); err != nil {
		t.Fatal(err)
	}

	_, err = d.BuildInvoiceLines(ctx, teamID, start.Add(-time.Hour), start.Add(24*time.Hour), 0)
	if err == nil || !strings.Contains(err.Error(), "parse invoice session") {
		t.Fatalf("BuildInvoiceLines error = %v, want malformed-session error", err)
	}
}
