package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// No path lets a draft go below zero or go out below zero: adding a discount,
// removing a fee from under a discount, marking it sent and queuing the email.
func TestInvoiceTotalNeverGoesBelowZero(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Итог", "invoice-total")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-950", Client: "Client", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	add := func(cents int) error {
		return d.AddInvoiceLine(ctx, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Label: "line", AmountCents: cents})
	}
	if err := add(100000); err != nil {
		t.Fatal(err)
	}
	if err := add(-90000); err != nil {
		t.Fatal(err)
	}
	if err := add(50000); err != nil {
		t.Fatal(err)
	}
	if err := add(-70000); !errors.Is(err, model.ErrInvoiceTotal) {
		t.Fatalf("discount below zero: err = %v", err)
	}

	// 1000 - 900 + 500 - 200: removing the 500 would leave -100.
	var feeID int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT id FROM invoice_lines WHERE invoice_id = ? AND amount_cents = 50000`, inv.ID).Scan(&feeID); err != nil {
		t.Fatal(err)
	}
	if err := add(-20000); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteInvoiceLine(ctx, appmodel.InvoiceLineDeleteRequest{TeamID: teamID, InvoiceID: inv.ID, LineID: feeID, CallerID: ownerID}); !errors.Is(err, model.ErrInvoiceTotal) {
		t.Fatalf("removing a fee from under a discount: err = %v", err)
	}

	// A negative draft however it came about cannot be sent or emailed.
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents) VALUES (?, 'x', '', 0, 0, -1000000)`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); !errors.Is(err, model.ErrInvoiceTotal) {
		t.Fatalf("mark sent: err = %v", err)
	}
	var revision int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT revision FROM invoices WHERE id = ?`, inv.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := d.EnqueueInvoiceEmail(ctx, appmodel.InvoiceEmailEnqueueRequest{
		TeamID: teamID, InvoiceID: inv.ID, Revision: revision, Recipient: "client@test.local", Payload: []byte("msg"),
	}); !errors.Is(err, model.ErrInvoiceTotal) {
		t.Fatalf("queue email: err = %v", err)
	}
}

// Adjustments added before payroll lines carried a kind are found once, by the
// " — " their label was built with; a salary or priced line is left alone.
func TestLegacyPayrollAdjustmentsAreMarkedOnce(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Старые", "legacy-adjustments")
	ownerID := teamOwner(t, d, teamID)
	var runID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, created_at, currency) VALUES (?, 'PAY-2026-090', '2026-09-01', '2026-09-30', 'draft', '', '2026-10-01', 'RUB') RETURNING id`,
		teamID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	for _, line := range []struct {
		label         string
		seconds, rate int
		amount        int
	}{
		{"Анна", 3600, 150000, 150000},
		{"Борис", 0, 0, 9000000},
		{"Анна — Премия", 0, 0, 500000},
	} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, ?, ?, ?)`,
			runID, ownerID, line.label, line.seconds, line.rate, line.amount); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = 'payroll adjustment kind'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migratePayrollAdjustmentKind(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := d.TestSQL().QueryContext(ctx, `SELECT label, kind FROM payroll_lines WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]string{"Анна": "", "Борис": "", "Анна — Премия": model.PayrollLineAdjustment}
	for rows.Next() {
		var label, kind string
		if err := rows.Scan(&label, &kind); err != nil {
			t.Fatal(err)
		}
		if kind != want[label] {
			t.Fatalf("%q has kind %q, want %q", label, kind, want[label])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
