package db

import (
	"database/sql"
	"errors"
	"fmt"
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
		{"Анна — Налог самозанятого (+6%)", 0, 0, 9000},
		{"Анна — Удержание (-1.50%)", 0, 0, -2250},
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
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = 'payroll percent from label'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migratePayrollPercentFromLabel(ctx); err != nil {
		t.Fatal(err)
	}
	var bps []int
	if err := queryRows(ctx, d.sql, "percent", `SELECT percent_bps FROM payroll_lines WHERE run_id = ? ORDER BY id`, func(r *sql.Rows) error {
		var v int
		if err := r.Scan(&v); err != nil {
			return err
		}
		bps = append(bps, v)
		return nil
	}, runID); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(bps) != "[0 0 0 600 -150]" {
		t.Fatalf("percent from label = %v", bps)
	}
	want := map[string]string{"Анна": "", "Борис": "", "Анна — Премия": model.PayrollLineAdjustment,
		"Анна — Налог самозанятого (+6%)": model.PayrollLineAdjustment, "Анна — Удержание (-1.50%)": model.PayrollLineAdjustment}
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

// An invoice whose seller or client numbers do not check out cannot go out.
// A draft carries the workspace's current details, so fixing them in the
// settings unblocks it without a rebuild.
func TestInvoiceWithBadDetailsCannotGoOut(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Реквизиты", "invoice-details")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE teams SET requisites = 'ИП Иванов, ИНН 7707083894' WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-960", Client: "Client", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	// CreateInvoice stores no copy of the seller's details; the production
	// draft path does. Put the copy in place as that path would.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET seller_details = 'ИП Иванов, ИНН 7707083894' WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	send := func() error {
		return d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID})
	}
	var bad *model.RequisitesError
	if err := send(); !errors.As(err, &bad) || bad.Client || len(bad.Fields) != 1 || bad.Fields[0] != model.RequisiteINN {
		t.Fatalf("bad seller ИНН: err = %v", err)
	}
	// Every other door a client could see the draft through is shut too:
	// paying it straight from the draft, a payment link and a client link.
	if _, _, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); !errors.As(err, &bad) {
		t.Fatalf("paid from draft: err = %v", err)
	}
	var revision int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT revision FROM invoices WHERE id = ?`, inv.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, ExpectedRevision: revision, PaymentURL: "https://pay.example/x"}); !errors.As(err, &bad) {
		t.Fatalf("payment link on draft: err = %v", err)
	}
	if err := d.CreateInvoiceShareLink(ctx, appmodel.InvoiceShareRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, TokenHash: "h", Now: start, ExpiresAt: start.AddDate(0, 0, 30)}); !errors.As(err, &bad) {
		t.Fatalf("client link to draft: err = %v", err)
	}
	// Fixing the number in the settings fixes the draft: no rebuild, so the
	// manual lines stay.
	if err := d.AddInvoiceLine(ctx, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Label: "Сервер", AmountCents: 50000}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTeamRequisites(ctx, appmodel.TeamRequisitesRequest{TeamID: teamID, CallerID: ownerID, Requisites: "ИП Иванов, ИНН 7707083893"}); err != nil {
		t.Fatalf("save requisites: %v", err)
	}
	var seller string
	var manual int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT seller_details, (SELECT COUNT(*) FROM invoice_lines WHERE invoice_id = invoices.id) FROM invoices WHERE id = ?`, inv.ID).Scan(&seller, &manual); err != nil {
		t.Fatal(err)
	}
	if seller != "ИП Иванов, ИНН 7707083893" || manual != 1 {
		t.Fatalf("draft after fixing the settings: seller %q, %d lines", seller, manual)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET client_details = 'БИК 044525225, к/с 30101810400000000226' WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := send(); !errors.As(err, &bad) || !bad.Client {
		t.Fatalf("bad client к/с: err = %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET client_details = '' WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := send(); err != nil {
		t.Fatalf("after fixing both sides: %v", err)
	}
}
