package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrStripeSessionMismatch = model.ErrStripeSessionMismatch
var ErrStripeSessionPending = model.ErrStripeSessionPending

// SetPaymentURL stores a Stripe Checkout (or manual) payment link only if the
// invoice still has the revision used to calculate the provider checkout.
func (d *DB) SetPaymentURL(ctx context.Context, request appmodel.InvoicePaymentLinkSaveRequest) error {
	teamID, id, callerID, expectedRevision := request.TeamID, request.InvoiceID, request.CallerID, request.ExpectedRevision
	paymentURL, stripeSession := request.PaymentURL, request.StripeSessionID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	var status, number string
	var revision int64
	if err := tx.QueryRowContext(ctx,
		`SELECT status, number, revision FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status, &number, &revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "paid" || status == "sending" {
		return ErrInvoiceNotDraft
	}
	if revision != expectedRevision {
		return model.ErrInvoiceChanged
	}
	now := d.currentTime().UTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET payment_url = ?, stripe_session_id = ? WHERE id = ? AND team_id = ?`,
		paymentURL, stripeSession, id, teamID); err != nil {
		return err
	}
	if stripeSession != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_stripe_sessions (stripe_session_id, team_id, invoice_id, created_at)
			 VALUES (?, ?, ?, ?)`, stripeSession, teamID, id, FormatTime(now)); err != nil {
			if isUniqueViolation(err) {
				return ErrDuplicate
			}
			return err
		}
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.PaymentLinkCreatedEvent{InvoiceID: id, Number: number, PaymentURL: paymentURL}, now); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkInvoicePaidOnce marks an invoice paid once and reports whether this call
// performed the transition. Callers use the result to avoid duplicate side
// effects when payment providers retry delivery.
func (d *DB) MarkInvoicePaidOnce(ctx context.Context, request appmodel.InvoiceMutationRequest) (string, bool, error) {
	teamID, id, callerID := request.TeamID, request.InvoiceID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return "", false, err
	}
	var status, number string
	if err := tx.QueryRowContext(ctx,
		`SELECT status, number FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status, &number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}
	if status == "paid" {
		return number, false, nil
	}
	transitionTime := d.currentTime().UTC()
	_, err = tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'paid', paid_at = ? WHERE id = ? AND team_id = ?`, FormatTime(transitionTime), id, teamID)
	if err != nil {
		return "", false, err
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.InvoicePaidEvent{InvoiceID: id, TeamID: teamID}, transitionTime); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return number, true, nil
}

// MarkInvoicePaidFromStripe applies a verified checkout event for any stored
// session belonging to this invoice. RebuildInvoice retires sessions whose
// prices may have changed; retries after payment remain idempotent.

func (d *DB) MarkInvoicePaidFromStripe(ctx context.Context, request appmodel.InvoiceStripePaymentRequest) (bool, error) {
	teamID, id, sessionID := request.TeamID, request.InvoiceID, request.StripeSessionID
	if teamID <= 0 || id <= 0 || strings.TrimSpace(sessionID) == "" {
		return false, ErrStripeSessionMismatch
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).
		Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	var sessionTeam, sessionInvoice, active int64
	if err := tx.QueryRowContext(ctx,
		`SELECT team_id, invoice_id, active FROM invoice_stripe_sessions WHERE stripe_session_id = ?`, sessionID).
		Scan(&sessionTeam, &sessionInvoice, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrStripeSessionPending
		}
		return false, err
	}
	if sessionTeam != teamID || sessionInvoice != id || active != 1 {
		return false, ErrStripeSessionMismatch
	}
	if status == "paid" {
		return false, nil
	}
	transitionTime := d.currentTime().UTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'paid', paid_at = ? WHERE id = ? AND team_id = ?`,
		FormatTime(transitionTime), id, teamID); err != nil {
		return false, err
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.InvoicePaidEvent{InvoiceID: id, TeamID: teamID}, transitionTime); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// billedCutoff is when sessions started carrying invoice_id. Invoices
// made before it get their sessions stamped once, by the same rule the
// old code billed with: same workspace, start inside the period, same
// "project · activity" line, and already existing when the invoice was
// made. Later invoices are stamped at creation, so this never re-runs on them.
// insertLines writes an invoice's lines and stamps the sessions they bill.
// SetInvoiceReceipt stores the "Мой налог" receipt (number or link).
func (d *DB) SetInvoiceReceipt(ctx context.Context, request appmodel.InvoiceReceiptRequest) error {
	teamID, id, callerID, receipt := request.TeamID, request.InvoiceID, request.CallerID, request.Receipt
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE invoices SET receipt = ?, revision = revision + 1 WHERE id = ? AND team_id = ?`, receipt, id, teamID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
