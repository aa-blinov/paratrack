package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// CreateInvoiceShareLink stores the hash of a client's link to an invoice, for a
// manager of the workspace (FIN-13).
func (d *DB) CreateInvoiceShareLink(ctx context.Context, request appmodel.InvoiceShareRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.TokenHash == "" {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM invoices WHERE id = ? AND team_id = ?`, request.InvoiceID, request.TeamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// A client link to a draft shows the client a document that was never
	// cleared to go out and can still change under the same address, so the
	// invoice is sent first; a voided one is not handed out again.
	if status == "draft" {
		if err := requireInvoiceIssuableTx(ctx, tx, request.TeamID, request.InvoiceID); err != nil {
			return err
		}
		return model.ErrInvoiceNotIssued
	}
	if status == "void" {
		return model.ErrInvoiceNotIssued
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO invoice_share_links (team_id, invoice_id, token_hash, created_by, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		request.TeamID, request.InvoiceID, request.TokenHash, request.CallerID,
		FormatTime(request.Now), FormatTime(request.ExpiresAt)); err != nil {
		return err
	}
	return tx.Commit()
}

// FindInvoiceShareLink opens a client's link: the workspace and the invoice it names,
// while the link has not expired. Anything else is not found.
func (d *DB) FindInvoiceShareLink(ctx context.Context, query appmodel.InvoiceShareLookupQuery) (appmodel.InvoiceShareLookup, error) {
	if query.TokenHash == "" {
		return appmodel.InvoiceShareLookup{}, ErrNotFound
	}
	var link appmodel.InvoiceShareLookup
	err := d.sql.QueryRowContext(ctx,
		`SELECT team_id, invoice_id FROM invoice_share_links WHERE token_hash = ? AND expires_at > ?`,
		query.TokenHash, FormatTime(query.Now)).Scan(&link.TeamID, &link.InvoiceID)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.InvoiceShareLookup{}, ErrNotFound
	}
	return link, err
}

// RevokeInvoiceShareLinks closes every client link to an invoice before it
// would expire, for a manager of the workspace, and says how many it closed.
func (d *DB) RevokeInvoiceShareLinks(ctx context.Context, request appmodel.InvoiceShareRevokeRequest) (int64, error) {
	if request.TeamID <= 0 || request.InvoiceID <= 0 {
		return 0, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM invoice_share_links WHERE team_id = ? AND invoice_id = ?`, request.TeamID, request.InvoiceID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// InvoiceHideNames reads whether a sent invoice was frozen with the
// executors' names hidden.
func (d *DB) InvoiceHideNames(ctx context.Context, query appmodel.InvoiceLookupQuery) (appmodel.InvoiceHideNames, error) {
	var hide sql.NullInt64
	err := d.sql.QueryRowContext(ctx, `SELECT hide_names FROM invoices WHERE id = ? AND team_id = ?`, query.InvoiceID, query.TeamID).Scan(&hide)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.InvoiceHideNames{}, ErrNotFound
	}
	if err != nil {
		return appmodel.InvoiceHideNames{}, err
	}
	return appmodel.InvoiceHideNames{Frozen: hide.Valid, Hide: hide.Valid && hide.Int64 != 0}, nil
}

// InvoiceIssuedSnapshot reads what an invoice looked like when it was issued,
// empty when nothing was kept.
func (d *DB) InvoiceIssuedSnapshot(ctx context.Context, query appmodel.InvoiceLookupQuery) (string, error) {
	var data string
	err := d.sql.QueryRowContext(ctx, `SELECT issued_snapshot FROM invoices WHERE id = ? AND team_id = ?`, query.InvoiceID, query.TeamID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return data, err
}

// SaveInvoiceIssuedSnapshot keeps the issued look of an invoice. A draft's is
// replaced on every send attempt; an issued invoice's is written only once.
func (d *DB) SaveInvoiceIssuedSnapshot(ctx context.Context, request appmodel.InvoiceSnapshotSaveRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET issued_snapshot = ?, issued_on = ? WHERE id = ? AND team_id = ? AND (status = 'draft' OR issued_snapshot = '')`,
		request.Data, request.IssuedOn, request.InvoiceID, request.TeamID)
	return err
}

// VoidInvoice cancels a sent invoice with nothing paid on it: the number
// stays used, the document is marked void with its reason, client links say
// it was voided, and its hours can be billed again. A payment has to be
// reversed first, and a month already closed is not changed.
func (d *DB) VoidInvoice(ctx context.Context, request appmodel.InvoiceVoidRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status, start, end string
	if err := tx.QueryRowContext(ctx,
		`SELECT status, period_start, period_end FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`,
		request.InvoiceID, request.TeamID).Scan(&status, &start, &end); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	paid, err := invoicePaidCentsOf(ctx, tx, request.TeamID, request.InvoiceID)
	if err != nil {
		return err
	}
	if status != "sent" || paid != 0 {
		return model.ErrInvoiceNotVoidable
	}
	from, err := ScanTime(start)
	if err != nil {
		return err
	}
	to, err := ScanTime(end)
	if err != nil {
		return err
	}
	if err := lockClosedPeriodSpan(ctx, tx, request.TeamID, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'void', void_reason = ?, voided_at = ? WHERE id = ? AND team_id = ?`,
		request.Reason, FormatTime(d.currentTime().UTC()), request.InvoiceID, request.TeamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ? AND team_id = ?`, request.InvoiceID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// InvoiceVoidReason reads why an invoice was voided, empty when it was not.
func (d *DB) InvoiceVoidReason(ctx context.Context, query appmodel.InvoiceLookupQuery) (string, error) {
	var reason string
	err := d.sql.QueryRowContext(ctx, `SELECT void_reason FROM invoices WHERE id = ? AND team_id = ?`, query.InvoiceID, query.TeamID).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return reason, err
}
