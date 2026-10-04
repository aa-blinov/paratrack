package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// DeleteInvoice removes a draft invoice and frees its billed sessions. The
// status check is performed under the invoice row lock so a stale delete
// cannot remove a document after it has been sent or paid.
func (d *DB) DeleteInvoice(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	teamID, id, callerID := request.TeamID, request.InvoiceID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invoice deletion: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	// Session edits lock their session first and then its invoice. Acquire the
	// linked sessions before the invoice here as well, or concurrent delete/edit
	// can deadlock while each transaction waits for the other's row.
	if err := lockInvoiceSessions(ctx, tx, teamID, id); err != nil {
		return err
	}
	var status, number string
	if err := tx.QueryRowContext(ctx,
		`SELECT status, number FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status, &number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock invoice for deletion: %w", err)
	}
	if status != "draft" {
		return ErrInvoiceNotDraft
	}
	// Its sessions become billable again only if the invoice deletion commits.
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ? AND team_id = ?`, id, teamID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM invoices WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func insertLines(ctx context.Context, tx *Tx, teamID, invID int64, lines []InvoiceLine) error {
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			invID, l.Label, l.Detail, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return err
		}
		if len(l.SessionIDs) == 0 {
			continue
		}
		// Only sessions still free take the stamp: if another invoice got
		// one first (two managers at once), this one fails whole.
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET invoice_id = ? WHERE id = ANY(?) AND invoice_id IS NULL AND team_id = ?`, invID, l.SessionIDs, teamID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != int64(len(l.SessionIDs)) {
			return ErrAlreadyBilled
		}
	}
	return nil
}

// ErrAlreadyBilled: some of the time was put on another invoice meanwhile.
var ErrAlreadyBilled = model.ErrAlreadyBilled

// RebuildInvoice recomputes a draft's lines from its period and project:
// edited sessions and newly finished ones are picked up, its own billed
// sessions are released first and re-stamped.
func (d *DB) RebuildInvoice(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	teamID, id, callerID := request.TeamID, request.InvoiceID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockInvoiceWorkspace(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	var startRaw, endRaw, status string
	var projectID int64
	var byPerson int
	if err := tx.QueryRowContext(ctx,
		`SELECT period_start, period_end, COALESCE(project_id, 0), by_person, status
		 FROM invoices WHERE id = ? AND team_id = ?`, id, teamID).
		Scan(&startRaw, &endRaw, &projectID, &byPerson, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := lockInvoiceSessions(ctx, tx, teamID, id); err != nil {
		return err
	}
	start, err := ScanTime(startRaw)
	if err != nil {
		return fmt.Errorf("read invoice start: %w", err)
	}
	end, err := ScanTime(endRaw)
	if err != nil {
		return fmt.Errorf("read invoice end: %w", err)
	}
	rules, err := billingRules(ctx, tx, teamID)
	if err != nil {
		return fmt.Errorf("load invoice billing rules: %w", err)
	}
	lines, err := buildInvoiceLines(ctx, tx, rules, teamID, start, end, projectID, id, d.currentTime(), invoiceLineBuildOptions{
		byPerson: byPerson == 1, lockRows: true,
	})
	if err != nil {
		return fmt.Errorf("build invoice lines: %w", err)
	}
	// Session locks are acquired before the invoice row to match edits that
	// lock a session and then its invoice. Recheck state after obtaining both.
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return ErrInvoiceNotDraft
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ? AND team_id = ?`, id, teamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_lines WHERE invoice_id = ?`, id); err != nil {
		return err
	}
	// Checkout sessions are valid for the invoice revision that created them.
	// A rebuild changes the amount, so retire every prior session atomically.
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoice_stripe_sessions SET active = 0 WHERE team_id = ? AND invoice_id = ?`, teamID, id); err != nil {
		return err
	}
	if err := insertLines(ctx, tx, teamID, id, lines); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET revision = revision + 1, payment_url = '', stripe_session_id = '' WHERE id = ? AND team_id = ?`, id, teamID); err != nil {
		return err
	}
	return tx.Commit()
}

func lockInvoiceSessions(ctx context.Context, tx *Tx, teamID, invoiceID int64) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM sessions WHERE team_id = ? AND invoice_id = ? ORDER BY id FOR UPDATE`,
		teamID, invoiceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

// UpdateInvoiceMetaAndProjectClient updates a draft invoice and its saved
// project-client defaults as one operation. The draft predicate protects
// against a concurrent send between the handler's read and this write.
func (d *DB) UpdateInvoiceMetaAndProjectClient(ctx context.Context, request appmodel.InvoiceDraftUpdateRequest) error {
	teamID, id, projectID, callerID := request.TeamID, request.InvoiceID, request.ProjectID, request.CallerID
	client, details, email, notes := request.Client, request.Details, request.Email, request.Notes
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invoice update: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE invoices SET client_name = ?, client_details = ?, client_email = ?, notes = ?, revision = revision + 1
		 WHERE id = ? AND team_id = ? AND status = 'draft'`,
		client, details, email, notes, id, teamID)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrInvoiceNotDraft
	}
	if projectID > 0 {
		res, err := tx.ExecContext(ctx,
			`UPDATE projects SET client_name = ?, client_details = ?, client_email = ? WHERE id = ? AND team_id = ?`,
			client, details, email, projectID, teamID)
		if err != nil {
			return fmt.Errorf("update project client defaults: %w", err)
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit()
}

// OverlappingInvoices lists other invoices whose period overlaps
// [start, end) and that bill any of the same lines (project · activity):
// the same hours may be on two documents.
func (d *DB) OverlappingInvoices(ctx context.Context, query appmodel.InvoiceOverlapQuery) ([]string, error) {
	if query.TeamID <= 0 || query.ExcludeInvoiceID <= 0 || query.Start.IsZero() || !query.End.After(query.Start) {
		return nil, ErrInvalidInvoiceQuery
	}
	want := map[string]bool{}
	for _, l := range query.Labels {
		want[l] = true
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT DISTINCT i.number, l.label FROM invoices i JOIN invoice_lines l ON l.invoice_id = i.id
		 WHERE i.team_id = ? AND i.id <> ? AND i.period_start < ? AND i.period_end > ?
		 ORDER BY i.number`,
		query.TeamID, query.ExcludeInvoiceID, FormatTime(query.End), FormatTime(query.Start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	seen := map[string]bool{}
	for rows.Next() {
		var num, label string
		if err := rows.Scan(&num, &label); err != nil {
			return nil, err
		}
		if want[label] && !seen[num] {
			seen[num] = true
			out = append(out, num)
		}
	}
	return out, rows.Err()
}

// ErrMixedCurrency: one document holds one currency; amounts in different
// currencies are never added up.
var ErrMixedCurrency = model.ErrMixedCurrency
var ErrInvoiceNotDraft = model.ErrInvoiceNotDraft

// MarkInvoiceSentOnce transitions a draft invoice to sent exactly once.
func (d *DB) MarkInvoiceSentOnce(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	teamID, id, callerID := request.TeamID, request.InvoiceID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'sent' WHERE id = ? AND team_id = ? AND status = 'draft'`, id, teamID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed > 0 {
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM invoices WHERE id = ? AND team_id = ?)`, id, teamID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return ErrInvoiceNotDraft
}

// invoiceCurrency is the single currency of the lines (each line carries
// its project's), or the workspace currency when lines don't say.
func invoiceCurrency(ctx context.Context, tx *Tx, teamID int64, lines []InvoiceLine) (string, error) {
	var team string
	if err := tx.QueryRowContext(ctx, `SELECT currency FROM teams WHERE id = ?`, teamID).Scan(&team); err != nil {
		return "", err
	}
	if team == "" {
		team = "RUB"
	}
	cur := ""
	for _, l := range lines {
		lc := l.Currency
		if lc == "" {
			lc = team // the project inherits the workspace currency
		}
		if cur != "" && lc != cur {
			return "", ErrMixedCurrency
		}
		cur = lc
	}
	if cur == "" {
		return team, nil
	}
	return cur, nil
}
