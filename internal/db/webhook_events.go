package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrWebhookEventLeaseLost = errors.New("webhook event lease was lost")

// recordWebhookEventTx writes a typed event beside its business change. It
// avoids rows when the workspace has no endpoint subscribed to this event.
func (d *DB) recordWebhookEventTx(ctx context.Context, tx *Tx, teamID int64, event webhookport.Event, createdAt time.Time) error {
	if teamID <= 0 || event == nil {
		return nil
	}
	eventName := event.EventName()
	rows, err := tx.QueryContext(ctx, `SELECT id, events FROM webhooks WHERE team_id = ? AND active = 1 FOR SHARE`, teamID)
	if err != nil {
		return fmt.Errorf("find webhook subscribers: %w", err)
	}
	defer rows.Close()
	var webhookIDs []int64
	for rows.Next() {
		var webhookID int64
		var filters string
		if err := rows.Scan(&webhookID, &filters); err != nil {
			_ = rows.Close()
			return err
		}
		if webhookport.Subscribes(filters, eventName) {
			webhookIDs = append(webhookIDs, webhookID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(webhookIDs) == 0 {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode webhook event %s: %w", eventName, err)
	}
	created := FormatTime(createdAt.UTC())
	legacyTargets := make([]string, len(webhookIDs))
	for i, webhookID := range webhookIDs {
		legacyTargets[i] = strconv.FormatInt(webhookID, 10)
	}
	var eventID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO webhook_event_outbox (team_id, event, payload, webhook_ids, attempts, available_at, created_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?) RETURNING id`, teamID, eventName, string(payload), strings.Join(legacyTargets, ","), created, created).Scan(&eventID); err != nil {
		return fmt.Errorf("record webhook event %s: %w", eventName, err)
	}
	for _, webhookID := range webhookIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO webhook_event_targets (event_id, webhook_id) VALUES (?, ?)`, eventID, webhookID); err != nil {
			return fmt.Errorf("record webhook event %s endpoint %d: %w", eventName, webhookID, err)
		}
	}
	return nil
}

// ClaimWebhookEvent leases one committed event for endpoint fan-out.
func (d *DB) ClaimWebhookEvent(ctx context.Context) (webhookport.CommittedEvent, bool, error) {
	now := d.currentTime().UTC()
	nowText := FormatTime(now)
	stale := FormatTime(now.Add(-5 * time.Minute))
	var event webhookport.CommittedEvent
	var payload, legacyTargets, created string
	err := d.sql.QueryRowContext(ctx, `
		WITH candidate AS (
		  SELECT id FROM webhook_event_outbox
		  WHERE (locked_at IS NULL AND available_at <= ?) OR locked_at < ?
		  ORDER BY available_at, id
		  LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_event_outbox e
		SET locked_at = ?, attempts = attempts + 1
		FROM candidate c WHERE e.id = c.id
		RETURNING e.id, e.team_id, e.event, e.payload, e.webhook_ids, e.attempts, e.locked_at, e.created_at`,
		nowText, stale, nowText).Scan(&event.ID, &event.TeamID, &event.Event, &payload, &legacyTargets, &event.Attempts, &event.Lease, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return webhookport.CommittedEvent{}, false, nil
	}
	if err != nil {
		return webhookport.CommittedEvent{}, false, err
	}
	event.Payload = []byte(payload)
	rows, err := d.sql.QueryContext(ctx, `SELECT webhook_id FROM webhook_event_targets WHERE event_id = ? ORDER BY webhook_id`, event.ID)
	if err != nil {
		return webhookport.CommittedEvent{}, false, fmt.Errorf("list webhook event targets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var webhookID int64
		if err := rows.Scan(&webhookID); err != nil {
			_ = rows.Close()
			return webhookport.CommittedEvent{}, false, err
		}
		event.WebhookIDs = append(event.WebhookIDs, webhookID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return webhookport.CommittedEvent{}, false, err
	}
	if err := rows.Close(); err != nil {
		return webhookport.CommittedEvent{}, false, err
	}
	if len(event.WebhookIDs) == 0 && legacyTargets != "" {
		for _, value := range strings.Split(legacyTargets, ",") {
			webhookID, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil || webhookID <= 0 {
				return webhookport.CommittedEvent{}, false, fmt.Errorf("parse legacy webhook target ID %q", value)
			}
			event.WebhookIDs = append(event.WebhookIDs, webhookID)
		}
	}
	event.CreatedAt, err = ScanTime(created)
	if err != nil {
		return webhookport.CommittedEvent{}, false, fmt.Errorf("parse webhook event time: %w", err)
	}
	return event, true, nil
}

// CompleteWebhookEvent removes an event after per-endpoint jobs have been
// inserted. The delivery queue's unique source key makes this operation safe
// to repeat after a worker crash.
func (d *DB) CompleteWebhookEvent(ctx context.Context, event webhookport.CommittedEvent) error {
	result, err := d.sql.ExecContext(ctx,
		`DELETE FROM webhook_event_outbox WHERE id = ? AND locked_at = ? AND attempts = ?`,
		event.ID, event.Lease, event.Attempts)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrWebhookEventLeaseLost
	}
	return nil
}

// RetryWebhookEvent releases a failed fan-out for a future worker attempt.

func (d *DB) RetryWebhookEvent(ctx context.Context, request appmodel.WebhookEventRetryRequest) error {
	event, availableAt := request.Event, request.AvailableAt
	result, err := d.sql.ExecContext(ctx,
		`UPDATE webhook_event_outbox SET locked_at = NULL, available_at = ?
		 WHERE id = ? AND locked_at = ? AND attempts = ?`,
		FormatTime(availableAt.UTC()), event.ID, event.Lease, event.Attempts)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrWebhookEventLeaseLost
	}
	return nil
}
