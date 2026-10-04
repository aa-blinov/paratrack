package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrWebhookDeliveryLeaseLost = errors.New("webhook delivery lease was lost")

// EnqueueWebhookDeliveries durably records one event for every endpoint that
// was selected by the webhook workflow. Deleted endpoints are skipped.

func (d *DB) EnqueueWebhookDeliveries(ctx context.Context, request appmodel.WebhookDeliveryBatchRequest) error {
	sourceEventID, teamID, webhookIDs, event, payload := request.SourceEventID, request.TeamID, request.WebhookIDs, request.Event, request.Payload
	if teamID <= 0 || strings.TrimSpace(event) == "" || len(payload) == 0 {
		return fmt.Errorf("invalid webhook delivery batch")
	}
	if sourceEventID < 0 {
		return fmt.Errorf("invalid source event ID")
	}
	if len(webhookIDs) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := FormatTime(d.currentTime().UTC())
	for _, webhookID := range webhookIDs {
		if webhookID <= 0 {
			return fmt.Errorf("invalid webhook endpoint ID")
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO webhook_delivery_queue (team_id, webhook_id, source_event_id, event, payload, attempts, available_at, created_at)
			 SELECT ?, id, ?, ?, ?, 0, ?, ? FROM webhooks WHERE id = ? AND team_id = ? AND active = 1
			 ON CONFLICT (source_event_id, webhook_id) WHERE source_event_id IS NOT NULL DO NOTHING`,
			teamID, nullableInt64(sourceEventID), event, string(payload), now, now, webhookID, teamID); err != nil {
			return fmt.Errorf("enqueue webhook delivery for endpoint %d: %w", webhookID, err)
		}
	}
	return tx.Commit()
}

// ClaimWebhookDelivery leases one ready row. A crashed worker's lease expires
// after five minutes so another process can retry it.
func (d *DB) ClaimWebhookDelivery(ctx context.Context) (webhookport.DeliveryJob, bool, error) {
	now := d.currentTime().UTC()
	nowText := FormatTime(now)
	stale := FormatTime(now.Add(-5 * time.Minute))
	var job webhookport.DeliveryJob
	var payload string
	var sealedSecret string
	err := d.sql.QueryRowContext(ctx, `
		WITH candidate AS (
		  SELECT q.id FROM webhook_delivery_queue q
		  JOIN webhooks w ON w.id = q.webhook_id AND w.active = 1
		  WHERE (q.locked_at IS NULL AND q.available_at <= ?) OR q.locked_at < ?
		  ORDER BY q.available_at, q.id
		  LIMIT 1 FOR UPDATE OF q SKIP LOCKED
		)
		UPDATE webhook_delivery_queue q
		SET locked_at = ?, attempts = attempts + 1
		FROM candidate c, webhooks w
		WHERE q.id = c.id AND w.id = q.webhook_id
		RETURNING q.id, q.team_id, q.webhook_id, w.url, w.secret, q.event, q.payload, q.attempts, q.locked_at`,
		nowText, stale, nowText).Scan(&job.ID, &job.TeamID, &job.WebhookID, &job.URL, &sealedSecret, &job.Event, &payload, &job.Attempts, &job.Lease)
	if errors.Is(err, sql.ErrNoRows) {
		return webhookport.DeliveryJob{}, false, nil
	}
	if err != nil {
		return webhookport.DeliveryJob{}, false, err
	}
	job.Secret = d.mustOpen(sealedSecret)
	job.Payload = []byte(payload)
	return job, true, nil
}

// CompleteWebhookDelivery deletes a delivered row. Attempt history is kept in
// webhook_deliveries, which intentionally omits request payloads.
func (d *DB) CompleteWebhookDelivery(ctx context.Context, job webhookport.DeliveryJob) error {
	result, err := d.sql.ExecContext(ctx,
		`DELETE FROM webhook_delivery_queue WHERE id = ? AND locked_at = ? AND attempts = ?`,
		job.ID, job.Lease, job.Attempts)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrWebhookDeliveryLeaseLost
	}
	return nil
}

// RetryWebhookDelivery releases a failed lease with a future availability
// time. Rows beyond the attempt limit are discarded after their final log.

func (d *DB) RetryWebhookDelivery(ctx context.Context, request webhookport.DeliveryRetryRequest) error {
	job, availableAt, maxAttempts := request.Job, request.AvailableAt, request.MaxAttempts
	var result sql.Result
	var err error
	if job.Attempts >= maxAttempts {
		result, err = d.sql.ExecContext(ctx,
			`DELETE FROM webhook_delivery_queue WHERE id = ? AND locked_at = ? AND attempts = ?`,
			job.ID, job.Lease, job.Attempts)
	} else {
		result, err = d.sql.ExecContext(ctx,
			`UPDATE webhook_delivery_queue SET locked_at = NULL, available_at = ?
			 WHERE id = ? AND locked_at = ? AND attempts = ?`,
			FormatTime(availableAt.UTC()), job.ID, job.Lease, job.Attempts)
	}
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrWebhookDeliveryLeaseLost
	}
	return nil
}
