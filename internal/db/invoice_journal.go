package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// invoiceTotalCents is what the invoice's lines add up to: the amount owed.
func invoiceTotalCents(ctx context.Context, q queryRower, invoiceID int64) (int64, error) {
	var total int64
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM invoice_lines WHERE invoice_id = ?`, invoiceID).Scan(&total)
	return total, err
}

// invoicePaidCentsOf is what the journal already holds for the invoice.
func invoicePaidCentsOf(ctx context.Context, q queryRower, teamID, invoiceID int64) (int64, error) {
	var paid int64
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM invoice_payments WHERE team_id = ? AND invoice_id = ?`,
		teamID, invoiceID).Scan(&paid)
	return paid, err
}

// recordRemainingPaymentTx books what the client still owes as one journal entry,
// so marking an invoice paid leaves a payment behind (FIN-08). Nothing is booked
// when the journal already covers the invoice.
func recordRemainingPaymentTx(ctx context.Context, tx *Tx, teamID, invoiceID int64, source string, at time.Time) error {
	total, err := invoiceTotalCents(ctx, tx, invoiceID)
	if err != nil {
		return err
	}
	paid, err := invoicePaidCentsOf(ctx, tx, teamID, invoiceID)
	if err != nil {
		return err
	}
	remaining := total - paid
	if remaining <= 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO invoice_payments (team_id, invoice_id, paid_on, amount_cents, note, source, created_at) VALUES (?, ?, ?, ?, '', ?, ?)`,
		teamID, invoiceID, at.Format("2006-01-02"), remaining, source, FormatTime(at))
	return err
}

// RecordInvoicePayment books a payment the manager typed in. When it covers what
// is still owed, the invoice becomes paid in the same transaction (FIN-08). Only a
// sent invoice takes payments: a draft is not owed yet, a paid one is settled.
func (d *DB) RecordInvoicePayment(ctx context.Context, request appmodel.InvoicePaymentRequest) (bool, error) {
	teamID, id, callerID := request.TeamID, request.InvoiceID, request.CallerID
	if teamID <= 0 || id <= 0 || callerID <= 0 {
		return false, ErrNotFound
	}
	if request.AmountCents <= 0 {
		return false, model.ErrPaymentAmount
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return false, err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if status != "sent" {
		return false, model.ErrInvoiceNotPayable
	}
	total, err := invoiceTotalCents(ctx, tx, id)
	if err != nil {
		return false, err
	}
	paid, err := invoicePaidCentsOf(ctx, tx, teamID, id)
	if err != nil {
		return false, err
	}
	remaining := total - paid
	if int64(request.AmountCents) > remaining {
		return false, model.ErrPaymentAmount
	}
	when := d.currentTime().UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO invoice_payments (team_id, invoice_id, paid_on, amount_cents, note, source, created_at) VALUES (?, ?, ?, ?, ?, 'manual', ?)`,
		teamID, id, request.PaidOn, request.AmountCents, request.Note, FormatTime(when)); err != nil {
		return false, err
	}
	if int64(request.AmountCents) < remaining {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'paid', paid_at = ? WHERE id = ? AND team_id = ?`, FormatTime(when), id, teamID); err != nil {
		return false, err
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.InvoicePaidEvent{InvoiceID: id, TeamID: teamID}, when); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ListInvoicePayments returns the journal of one invoice, or of every invoice of
// the workspace when invoiceID is zero, in the order the money arrived.
func (d *DB) ListInvoicePayments(ctx context.Context, teamID, invoiceID int64) ([]model.InvoicePayment, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT id, team_id, invoice_id, paid_on, amount_cents, note, source, created_at FROM invoice_payments WHERE team_id = ?`
	args := []any{teamID}
	if invoiceID > 0 {
		q += ` AND invoice_id = ?`
		args = append(args, invoiceID)
	}
	q += ` ORDER BY paid_on, id`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InvoicePayment
	for rows.Next() {
		var p model.InvoicePayment
		var created string
		if err := rows.Scan(&p.ID, &p.TeamID, &p.InvoiceID, &p.PaidOn, &p.AmountCents, &p.Note, &p.Source, &created); err != nil {
			return nil, err
		}
		if p.CreatedAt, err = ScanTime(created); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// attachPayments hands each invoice its journal, read once for the whole list.
func (d *DB) attachPayments(ctx context.Context, teamID int64, details []appmodel.InvoiceDetails) ([]appmodel.InvoiceDetails, error) {
	payments, err := d.ListInvoicePayments(ctx, teamID, 0)
	if err != nil {
		return nil, err
	}
	byInvoice := make(map[int64][]model.InvoicePayment)
	for _, p := range payments {
		byInvoice[p.InvoiceID] = append(byInvoice[p.InvoiceID], p)
	}
	for i := range details {
		details[i].Payments = byInvoice[details[i].Invoice.ID]
	}
	return details, nil
}
