package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A pay run blocks reopening an approved week only when its period overlaps the
// week. A run that ends exactly where the week starts, or starts exactly where it
// ends, leaves the approval removable.
func TestReopenWeekRefusedOnlyByAPayRunThatOverlapsIt(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "reopen-boundary")
	weekStart := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	approve := func() {
		t.Helper()
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at) VALUES (?, ?, ?, ?, ?)`,
			ws.teamID, FormatTime(weekStart), FormatTime(weekEnd), ws.ownerID, FormatTime(weekEnd)); err != nil {
			t.Fatalf("approve the week: %v", err)
		}
	}
	payRun := func(number string, start, end time.Time) {
		t.Helper()
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, currency, created_at)
			 VALUES (?, ?, ?, ?, 'paid', '', 'RUB', ?)`,
			ws.teamID, number, FormatTime(start), FormatTime(end), FormatTime(weekEnd)); err != nil {
			t.Fatalf("seed pay run %s: %v", number, err)
		}
	}
	reopen := func() error {
		return d.ReopenTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Start: weekStart, End: weekEnd})
	}
	clearRuns := func(numbers ...string) {
		t.Helper()
		for _, n := range numbers {
			if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM payroll_runs WHERE team_id = ? AND number = ?`, ws.teamID, n); err != nil {
				t.Fatalf("remove pay run %s: %v", n, err)
			}
		}
	}

	// A run that ends where the week starts does not cover it.
	approve()
	payRun("PAY-EDGE-BEFORE", weekStart.AddDate(0, 0, -7), weekStart)
	if err := reopen(); err != nil {
		t.Fatalf("reopen with a run that ends at the week start: %v, want nil", err)
	}
	clearRuns("PAY-EDGE-BEFORE")

	// A run that starts where the week ends does not cover it either.
	approve()
	payRun("PAY-EDGE-AFTER", weekEnd, weekEnd.AddDate(0, 0, 7))
	if err := reopen(); err != nil {
		t.Fatalf("reopen with a run that starts at the week end: %v, want nil", err)
	}
	clearRuns("PAY-EDGE-AFTER")

	// A run that reaches one day into the week covers it.
	approve()
	payRun("PAY-OVERLAP", weekStart.AddDate(0, 0, -7), weekStart.AddDate(0, 0, 1))
	if err := reopen(); !errors.Is(err, model.ErrWeekPaid) {
		t.Fatalf("reopen with a run that overlaps the week = %v, want ErrWeekPaid", err)
	}
	clearRuns("PAY-OVERLAP")
}

// A draft invoice's lines were priced from the sessions as they were. Editing one
// session's time moves the tracked sum away from the lines; rebuilding the draft
// brings the lines back to the tracked sum. A sent invoice's sessions do not move.
func TestDraftInvoiceTracksItsSessionsAndRebuildClearsTheDrift(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "invoice-drift")
	owner := requestctx.WithActor(ctx, ws.ownerID)
	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT id FROM invoices WHERE team_id = ? ORDER BY id LIMIT 1`, ws.teamID).Scan(&invoiceID); err != nil {
		t.Fatalf("find an invoice: %v", err)
	}
	// Start from a draft whose lines match its one session exactly, so the drift is
	// only the one the test makes.
	for _, q := range []string{
		`UPDATE invoices SET status = 'draft' WHERE id = ?`,
		`DELETE FROM invoice_lines WHERE invoice_id = ?`,
		`UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ?`,
	} {
		if _, err := d.TestSQL().ExecContext(ctx, q, invoiceID); err != nil {
			t.Fatalf("reset the invoice: %v", err)
		}
	}
	// The seeded approvals are date-only, which the product never writes; the
	// drift test has no use for them.
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM timesheet_approvals WHERE team_id = ?`, ws.teamID); err != nil {
		t.Fatalf("clear the seeded approvals: %v", err)
	}
	// The period is written the way the product writes it, so a rebuild can read it;
	// the session falls inside it.
	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET period_start = ?, period_end = ? WHERE id = ?`,
		FormatTime(periodStart), FormatTime(periodStart.AddDate(0, 1, 0)), invoiceID); err != nil {
		t.Fatalf("write the invoice period: %v", err)
	}
	start := periodStart.Add(9 * time.Hour)
	sid := sessionFor(t, d, ws.teamID, ws.activityID, ws.ownerID, start)
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, sid); err != nil {
		t.Fatalf("bill the session to the draft: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents) VALUES (?, 'Работа', '', 3600, 0, 0)`,
		invoiceID); err != nil {
		t.Fatalf("write the line the draft was priced from: %v", err)
	}
	linesSeconds := func() int {
		var n int64
		if err := d.TestSQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds), 0) FROM invoice_lines WHERE invoice_id = ?`, invoiceID).Scan(&n); err != nil {
			t.Fatalf("sum the lines: %v", err)
		}
		return int(n)
	}
	tracked := func() int {
		n, err := d.TrackedSecondsForInvoice(ctx, ws.teamID, invoiceID)
		if err != nil {
			t.Fatalf("sum the tracked time: %v", err)
		}
		return n
	}

	if got, want := tracked(), linesSeconds(); got != want {
		t.Fatalf("a fresh draft tracks %d seconds against %d in its lines, want no drift", got, want)
	}

	half := 1800
	if err := d.UpdateSessionFields(owner, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, SessionID: sid,
		Update: appmodel.SessionUpdate{AccumulatedSeconds: &half, UpdatedAt: start},
	}); err != nil {
		t.Fatalf("correct the session on the draft: %v", err)
	}
	if got := tracked(); got != 1800 {
		t.Fatalf("after the correction the tracked time is %d, want 1800", got)
	}
	if got := linesSeconds(); got != 3600 {
		t.Fatalf("the lines moved to %d without a rebuild, want 3600", got)
	}

	if err := d.RebuildInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: ws.teamID, InvoiceID: invoiceID, CallerID: ws.ownerID}); err != nil {
		t.Fatalf("rebuild the draft: %v", err)
	}
	if got, want := linesSeconds(), tracked(); got != want {
		t.Fatalf("after the rebuild the lines hold %d seconds and the sessions %d", got, want)
	}

	// A sent invoice keeps its sessions: the correction is refused.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET status = 'sent' WHERE id = ?`, invoiceID); err != nil {
		t.Fatalf("send the invoice: %v", err)
	}
	again := tracked()
	if err := d.UpdateSessionFields(owner, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, SessionID: sid,
		Update: appmodel.SessionUpdate{AccumulatedSeconds: &again, UpdatedAt: start},
	}); err == nil {
		t.Fatalf("a sent invoice's session was corrected")
	}
}
