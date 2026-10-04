package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

var ErrInvoiceChanged = model.ErrInvoiceChanged
var ErrInvoiceEmailLeaseLost = errors.New("invoice email lease was lost")

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	return strings.ToValidUTF8(value[:maxBytes], "")
}

// EnqueueInvoiceEmail freezes a draft and records its prepared message as one
// transaction. The revision check rejects a stale PDF prepared before a
// concurrent edit or rebuild.

func (d *DB) EnqueueInvoiceEmail(ctx context.Context, request appmodel.InvoiceEmailEnqueueRequest) error {
	teamID, invoiceID, revision := request.TeamID, request.InvoiceID, request.Revision
	recipient, payload := request.Recipient, request.Payload
	if teamID <= 0 || invoiceID <= 0 || revision <= 0 || strings.TrimSpace(recipient) == "" || len(payload) == 0 {
		return fmt.Errorf("invalid invoice email request")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, actorID(ctx)); err != nil {
		return err
	}
	var status string
	var currentRevision int64
	if err := tx.QueryRowContext(ctx,
		`SELECT status, revision FROM invoices WHERE id = ? AND team_id = ? FOR UPDATE`, invoiceID, teamID).
		Scan(&status, &currentRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return ErrInvoiceNotDraft
	}
	if currentRevision != revision {
		return ErrInvoiceChanged
	}
	now := FormatTime(d.currentTime().UTC())
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = 'sending', client_email = ? WHERE id = ? AND team_id = ? AND status = 'draft'`,
		recipient, invoiceID, teamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO invoice_email_outbox (team_id, invoice_id, recipient, payload, status, attempts, available_at, created_at)
		 VALUES (?, ?, ?, ?, 'pending', 0, ?, ?)`, teamID, invoiceID, recipient, string(payload), now, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimInvoiceEmail leases one ready task. A worker crash leaves a finite
// lease; another worker can safely claim it after five minutes.
func (d *DB) ClaimInvoiceEmail(ctx context.Context) (mailport.InvoiceEmailJob, bool, error) {
	now := d.currentTime().UTC()
	nowText := FormatTime(now)
	stale := FormatTime(now.Add(-5 * time.Minute))
	var job mailport.InvoiceEmailJob
	var payload string
	err := d.sql.QueryRowContext(ctx, `
		WITH candidate AS (
		  SELECT id FROM invoice_email_outbox
		  WHERE (status = 'pending' AND available_at <= ?)
	     OR (status = 'processing' AND locked_at < ?)
		  ORDER BY available_at, id
		  LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		UPDATE invoice_email_outbox q
		SET status = 'processing', locked_at = ?, attempts = attempts + 1
		FROM candidate c WHERE q.id = c.id
		RETURNING q.id, q.team_id, q.invoice_id, q.recipient, q.payload, q.attempts, q.locked_at`,
		nowText, stale, nowText).Scan(&job.ID, &job.TeamID, &job.InvoiceID, &job.Recipient, &payload, &job.Attempts, &job.Lease)
	if errors.Is(err, sql.ErrNoRows) {
		return mailport.InvoiceEmailJob{}, false, nil
	}
	if err != nil {
		return mailport.InvoiceEmailJob{}, false, err
	}
	job.Payload = []byte(payload)
	return job, true, nil
}

// CompleteInvoiceEmail records successful delivery and finalizes the
// invoice's send state atomically. A payment received while email was queued
// remains paid.
func (d *DB) CompleteInvoiceEmail(ctx context.Context, job mailport.InvoiceEmailJob) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE invoice_email_outbox SET status = 'delivered', payload = '', locked_at = NULL, last_error = '', completed_at = ?
		 WHERE id = ? AND status = 'processing' AND locked_at = ? AND attempts = ?`, FormatTime(d.currentTime().UTC()), job.ID, job.Lease, job.Attempts)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrInvoiceEmailLeaseLost
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invoices SET status = CASE WHEN status = 'sending' THEN 'sent' ELSE status END
		 WHERE id = ? AND team_id = ?`, job.InvoiceID, job.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// RetryInvoiceEmail reschedules failed delivery with backoff. After the
// attempt limit, the draft is unlocked for a new user initiated send.

func (d *DB) RetryInvoiceEmail(ctx context.Context, request mailport.InvoiceEmailRetryRequest) error {
	job, message, availableAt, maxAttempts := request.Job, request.Message, request.AvailableAt, request.MaxAttempts
	message = truncateUTF8(message, 2000)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if job.Attempts >= maxAttempts {
		res, err := tx.ExecContext(ctx,
			`UPDATE invoice_email_outbox SET status = 'failed', payload = '', locked_at = NULL, last_error = ?
			 WHERE id = ? AND status = 'processing' AND locked_at = ? AND attempts = ?`, message, job.ID, job.Lease, job.Attempts)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrInvoiceEmailLeaseLost
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE invoices SET status = 'draft' WHERE id = ? AND team_id = ? AND status = 'sending'`, job.InvoiceID, job.TeamID); err != nil {
			return err
		}
	} else {
		res, err := tx.ExecContext(ctx,
			`UPDATE invoice_email_outbox SET status = 'pending', locked_at = NULL, available_at = ?, last_error = ?
			 WHERE id = ? AND status = 'processing' AND locked_at = ? AND attempts = ?`, FormatTime(availableAt), message, job.ID, job.Lease, job.Attempts)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrInvoiceEmailLeaseLost
		}
	}
	return tx.Commit()
}
