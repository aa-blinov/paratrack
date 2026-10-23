package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The reconciliation sets the time worked for a client against what its invoices bill
// (FIN-04): four hours worked, two of them on an invoice whose line bills one hour, and two
// on no invoice, leave one hour of difference.
func TestReconciliationSetsWorkedAgainstBilled(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Сверка", "reconcile-clients")
	ownerID := teamOwner(t, d, teamID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	var clientID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name) VALUES (?, 'Ромашка') RETURNING id`, teamID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE projects SET client_id = ? WHERE id = ?`, clientID, proj.ID); err != nil {
		t.Fatal(err)
	}
	activity := newTestActivity(t, d, ctx, teamID, ownerID, "Вёрстка")
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity, ProjectID: proj.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, activity, day.Add(9*time.Hour))
	var billedSession int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds)
		 VALUES (?, ?, ?, ?, ?, 7200) RETURNING id`,
		activity, teamID, ownerID, FormatTime(day.Add(12*time.Hour)), FormatTime(day.Add(14*time.Hour))).Scan(&billedSession); err != nil {
		t.Fatal(err)
	}
	seedClosedSession(t, d, teamID, ownerID, activity, day.Add(16*time.Hour))
	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-RECON', 'Ромашка', ?, ?, 'sent', ?) RETURNING id`,
		teamID, FormatTime(day), FormatTime(day.AddDate(0, 0, 7)), FormatTime(day)).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, billedSession); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO invoice_lines (invoice_id, label, seconds, rate_cents, amount_cents) VALUES (?, 'Сайт · Вёрстка', 3600, 300000, 300000)`, invoiceID); err != nil {
		t.Fatal(err)
	}

	rows, err := d.ReconcileClients(ctx, appmodel.ReconcileQuery{TeamID: teamID, From: day, Until: day.AddDate(0, 0, 1), Now: day.AddDate(0, 0, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ClientName != "Ромашка" {
		t.Fatalf("the reconciliation holds %+v, want the one client", rows)
	}
	got := rows[0]
	if got.WorkedSeconds != 4*3600 || got.BilledSeconds != 3600 || got.NotBilledSeconds != 2*3600 || got.DiffSeconds != 3600 {
		t.Fatalf("worked %d, billed %d, not billed %d, difference %d; want 4h, 1h, 2h, 1h", got.WorkedSeconds, got.BilledSeconds, got.NotBilledSeconds, got.DiffSeconds)
	}
}
