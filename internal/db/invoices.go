package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// Invoice persistence and reads.

// Shared invoice records are defined in the application model package.
type Invoice = model.Invoice
type InvoiceLine = model.InvoiceLine
type InvoiceOptions = appmodel.InvoiceOptions
type UnbilledProject = model.UnbilledProject

// CreateInvoice builds an invoice with its lines in one shot.
func (d *DB) CreateInvoice(ctx context.Context, request appmodel.InvoiceCreateRequest) (Invoice, error) {
	teamID, callerID := request.TeamID, request.CallerID
	number, clientName := request.Number, request.Client
	start, end, notes, lines := request.Start, request.End, request.Notes, request.Lines
	if number == "" {
		return Invoice{}, fmt.Errorf("number is required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return Invoice{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return Invoice{}, err
	}
	currency, err := invoiceCurrency(ctx, tx, teamID, lines)
	if err != nil {
		return Invoice{}, err
	}

	now := FormatTime(d.currentTime().UTC())
	var invID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, notes, currency, created_at)
		 VALUES (?, ?, ?, ?, ?, 'draft', ?, ?, ?) RETURNING id`,
		teamID, number, clientName, FormatTime(start), FormatTime(end), notes, currency, now).Scan(&invID)
	if err != nil {
		if isUniqueViolation(err) {
			return Invoice{}, ErrDuplicate
		}
		return Invoice{}, err
	}
	if err := insertLines(ctx, tx, teamID, invID, lines); err != nil {
		return Invoice{}, err
	}
	invoice, err := getInvoice(ctx, tx, teamID, invID, true)
	if err != nil {
		return Invoice{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invoice{}, err
	}
	return invoice, nil
}

// NextInvoiceNumber returns "INV-YYYY-NNN" for the team.
func (d *DB) NextInvoiceNumber(ctx context.Context, teamID int64) (string, error) {
	return d.nextInvoiceNumber(ctx, teamID, d.currentTime())
}

func (d *DB) nextInvoiceNumber(ctx context.Context, teamID int64, at time.Time) (string, error) {
	rules, err := d.TeamBilling(ctx, teamID)
	if err != nil {
		return "", err
	}
	return nextInvoiceNumberWithRules(ctx, d.sql, teamID, rules, at)
}

// nextInvoiceNumberWithRules uses its caller's queryer so invoice creation
// never checks out a second pool connection while holding the workspace lock.
func nextInvoiceNumberWithRules(ctx context.Context, queryer invoiceQueryer, teamID int64, rules BillingRules, at time.Time) (string, error) {
	year := at.Year()
	prefix := fmt.Sprintf("%s-%d-", rules.InvoicePrefix, year)
	var maxNum int
	err := queryer.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(CAST(SUBSTR(number, LENGTH(?) + 1) AS INTEGER)), 0)
		 FROM invoices WHERE team_id = ? AND number LIKE ?`,
		prefix, teamID, prefix+"%").Scan(&maxNum)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", prefix, maxNum+1), nil
}

// ListInvoices returns the team's invoices, newest first.
func (d *DB) ListInvoices(ctx context.Context, teamID int64) ([]Invoice, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), seller_details, client_details, vat_note, COALESCE(project_id, 0), client_email, receipt, by_person, created_at, revision
		 FROM invoices WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// GetInvoice fetches one invoice inside a team.
func (d *DB) GetInvoice(ctx context.Context, teamID, id int64) (Invoice, error) {
	return getInvoice(ctx, d.sql, teamID, id, false)
}

func getInvoice(ctx context.Context, queryer invoiceQueryer, teamID, id int64, lock bool) (Invoice, error) {
	query := `SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), seller_details, client_details, vat_note, COALESCE(project_id, 0), client_email, receipt, by_person, created_at, revision
		 FROM invoices WHERE id = ? AND team_id = ?`
	if lock {
		query += ` FOR UPDATE`
	}
	row := queryer.QueryRowContext(ctx, query, id, teamID)
	inv, err := scanInvoice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Invoice{}, ErrNotFound
	}
	return inv, err
}

// listInvoiceLines loads line items for an invoice created or scoped by its caller.
func listInvoiceLines(ctx context.Context, queryer invoiceQueryer, invoiceID int64) ([]InvoiceLine, error) {
	rows, err := queryer.QueryContext(ctx,
		`SELECT id, invoice_id, label, detail, seconds, rate_cents, amount_cents
		 FROM invoice_lines WHERE invoice_id = ? ORDER BY id`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InvoiceLine
	for rows.Next() {
		var l InvoiceLine
		if err := rows.Scan(&l.ID, &l.InvoiceID, &l.Label, &l.Detail, &l.Seconds, &l.RateCents, &l.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreateInvoiceDraft snapshots current billable time and stamps the selected
// sessions within the same transaction as the invoice and line inserts.
func (d *DB) CreateInvoiceDraft(ctx context.Context, request appmodel.InvoiceDraftRequest) (Invoice, []InvoiceLine, error) {
	teamID, callerID := request.TeamID, request.CallerID
	clientName, notes := request.Client, request.Notes
	start, end := request.Start, request.End
	options := request.Options
	projectID, byPerson := options.ProjectID, options.ByPerson
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return Invoice{}, nil, fmt.Errorf("begin invoice creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockInvoiceWorkspace(ctx, tx, teamID, callerID); err != nil {
		return Invoice{}, nil, err
	}
	now := d.currentTime()
	rules, err := billingRules(ctx, tx, teamID)
	if err != nil {
		return Invoice{}, nil, fmt.Errorf("load invoice rules: %w", err)
	}
	lines, err := buildInvoiceLines(ctx, tx, rules, teamID, start, end, projectID, 0, now, invoiceLineBuildOptions{
		byPerson: byPerson, lockRows: true,
	})
	if err != nil {
		return Invoice{}, nil, fmt.Errorf("build invoice lines: %w", err)
	}
	if len(lines) == 0 {
		return Invoice{}, nil, model.ErrNoBillableTime
	}
	currency, err := invoiceCurrency(ctx, tx, teamID, lines)
	if err != nil {
		return Invoice{}, nil, err
	}
	number, err := nextInvoiceNumberWithRules(ctx, tx, teamID, rules, now)
	if err != nil {
		return Invoice{}, nil, fmt.Errorf("allocate invoice number: %w", err)
	}
	var sellerDetails, vatNote string
	if err := tx.QueryRowContext(ctx, `SELECT requisites, vat_note FROM teams WHERE id = ?`, teamID).Scan(&sellerDetails, &vatNote); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Invoice{}, nil, ErrNotFound
		}
		return Invoice{}, nil, err
	}
	createdAt := FormatTime(now.UTC())
	var invoiceID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, notes,
			currency, seller_details, client_details, vat_note, project_id, client_email, by_person, created_at, revision)
		VALUES (?, ?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?, ?, ?, ?, 1) RETURNING id`,
		teamID, number, strings.TrimSpace(clientName), FormatTime(start), FormatTime(end), strings.TrimSpace(notes),
		currency, sellerDetails, strings.TrimSpace(options.ClientDetails), vatNote,
		nullableInt64(projectID), strings.TrimSpace(options.ClientEmail), boolInt(byPerson), createdAt,
	).Scan(&invoiceID)
	if err != nil {
		if isUniqueViolation(err) {
			return Invoice{}, nil, ErrDuplicate
		}
		return Invoice{}, nil, err
	}
	if err := insertLines(ctx, tx, teamID, invoiceID, lines); err != nil {
		return Invoice{}, nil, err
	}
	if options.RememberClient && projectID > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE projects SET client_name = ?, client_details = ?, client_email = ? WHERE id = ? AND team_id = ?`,
			strings.TrimSpace(clientName), strings.TrimSpace(options.ClientDetails), strings.TrimSpace(options.ClientEmail), projectID, teamID); err != nil {
			return Invoice{}, nil, fmt.Errorf("remember project client: %w", err)
		}
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.InvoiceCreatedEvent{InvoiceID: invoiceID, Number: number, Client: strings.TrimSpace(clientName)}, now); err != nil {
		return Invoice{}, nil, err
	}
	inv, err := getInvoice(ctx, tx, teamID, invoiceID, true)
	if err != nil {
		return Invoice{}, nil, err
	}
	storedLines, err := listInvoiceLines(ctx, tx, invoiceID)
	if err != nil {
		return Invoice{}, nil, err
	}
	for i := range storedLines {
		if i < len(lines) {
			storedLines[i].Currency = lines[i].Currency
			storedLines[i].ProjectID = lines[i].ProjectID
		}
	}
	if err := tx.Commit(); err != nil {
		return Invoice{}, nil, fmt.Errorf("commit invoice creation: %w", err)
	}
	return inv, storedLines, nil
}

// ListInvoiceDetails loads invoice snapshots with their lines for one team.
func (d *DB) ListInvoiceDetails(ctx context.Context, teamID int64) ([]model.InvoiceDetails, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT i.id, i.team_id, i.number, i.client_name, i.period_start, i.period_end, i.status, i.notes,
			i.payment_url, COALESCE(NULLIF(i.currency, ''), (SELECT t.currency FROM teams t WHERE t.id = i.team_id), 'RUB'),
			i.seller_details, i.client_details, i.vat_note, COALESCE(i.project_id, 0), i.client_email,
			i.receipt, i.by_person, i.created_at, i.revision,
			l.id, l.invoice_id, l.label, l.detail, l.seconds, l.rate_cents, l.amount_cents
		FROM invoices i LEFT JOIN invoice_lines l ON l.invoice_id = i.id
		WHERE i.team_id = ? ORDER BY i.created_at DESC, l.id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInvoiceDetails(rows)
}

func scanInvoiceDetails(rows *sql.Rows) ([]model.InvoiceDetails, error) {
	details := make([]model.InvoiceDetails, 0)
	indices := make(map[int64]int)
	var err error
	for rows.Next() {
		var inv Invoice
		var start, end, created string
		var paymentURL sql.NullString
		var byPerson int
		var lineID, lineInvoiceID, seconds, rate, amount sql.NullInt64
		var label, detail sql.NullString
		if err := rows.Scan(&inv.ID, &inv.TeamID, &inv.Number, &inv.ClientName,
			&start, &end, &inv.Status, &inv.Notes, &paymentURL, &inv.Currency,
			&inv.SellerDetails, &inv.ClientDetails, &inv.VATNote, &inv.ProjectID,
			&inv.ClientEmail, &inv.Receipt, &byPerson, &created, &inv.Revision,
			&lineID, &lineInvoiceID, &label, &detail, &seconds, &rate, &amount); err != nil {
			return nil, err
		}
		inv.PeriodStart, err = ScanTime(start)
		if err != nil {
			return nil, fmt.Errorf("parse invoice period start: %w", err)
		}
		inv.PeriodEnd, err = ScanTime(end)
		if err != nil {
			return nil, fmt.Errorf("parse invoice period end: %w", err)
		}
		inv.CreatedAt, err = ScanTime(created)
		if err != nil {
			return nil, fmt.Errorf("parse invoice creation time: %w", err)
		}
		inv.PaymentURL = paymentURL.String
		inv.ByPerson = byPerson == 1
		index, ok := indices[inv.ID]
		if !ok {
			index = len(details)
			indices[inv.ID] = index
			details = append(details, model.InvoiceDetails{Invoice: inv})
		}
		if lineID.Valid {
			details[index].Lines = append(details[index].Lines, InvoiceLine{
				ID: lineID.Int64, InvoiceID: lineInvoiceID.Int64, Label: label.String,
				Detail: detail.String, Seconds: int(seconds.Int64), RateCents: int(rate.Int64), AmountCents: int(amount.Int64),
			})
		}
	}
	return details, rows.Err()
}

// GetInvoiceDetails returns an invoice and its frozen lines, scoped to a team.
func (d *DB) GetInvoiceDetails(ctx context.Context, teamID, invoiceID int64) (model.InvoiceDetails, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT i.id, i.team_id, i.number, i.client_name, i.period_start, i.period_end, i.status, i.notes,
			i.payment_url, COALESCE(NULLIF(i.currency, ''), (SELECT t.currency FROM teams t WHERE t.id = i.team_id), 'RUB'),
			i.seller_details, i.client_details, i.vat_note, COALESCE(i.project_id, 0), i.client_email,
			i.receipt, i.by_person, i.created_at, i.revision,
			l.id, l.invoice_id, l.label, l.detail, l.seconds, l.rate_cents, l.amount_cents
		FROM invoices i LEFT JOIN invoice_lines l ON l.invoice_id = i.id
		WHERE i.team_id = ? AND i.id = ? ORDER BY l.id`, teamID, invoiceID)
	if err != nil {
		return model.InvoiceDetails{}, err
	}
	defer rows.Close()
	details, err := scanInvoiceDetails(rows)
	if err != nil {
		return model.InvoiceDetails{}, fmt.Errorf("load invoice details: %w", err)
	}
	if len(details) == 0 {
		return model.InvoiceDetails{}, ErrNotFound
	}
	return details[0], nil
}

func lockInvoiceWorkspace(ctx context.Context, tx *Tx, teamID, callerID int64) error {
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	return nil
}

func scanInvoice(row interface{ Scan(...any) error }) (Invoice, error) {
	var inv Invoice
	var start, end, created string
	var paymentURL sql.NullString
	var byPerson int
	err := row.Scan(&inv.ID, &inv.TeamID, &inv.Number, &inv.ClientName,
		&start, &end, &inv.Status, &inv.Notes, &paymentURL, &inv.Currency,
		&inv.SellerDetails, &inv.ClientDetails, &inv.VATNote, &inv.ProjectID,
		&inv.ClientEmail, &inv.Receipt, &byPerson, &created, &inv.Revision)
	if err != nil {
		return Invoice{}, err
	}
	inv.PeriodStart, err = ScanTime(start)
	if err != nil {
		return Invoice{}, fmt.Errorf("parse invoice period start: %w", err)
	}
	inv.PeriodEnd, err = ScanTime(end)
	if err != nil {
		return Invoice{}, fmt.Errorf("parse invoice period end: %w", err)
	}
	inv.CreatedAt, err = ScanTime(created)
	if err != nil {
		return Invoice{}, fmt.Errorf("parse invoice creation time: %w", err)
	}
	inv.PaymentURL = paymentURL.String
	inv.ByPerson = byPerson == 1
	return inv, nil
}
