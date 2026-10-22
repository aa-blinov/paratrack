package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
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
	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM invoices WHERE id = ? AND team_id = ?`, request.InvoiceID, request.TeamID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
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
