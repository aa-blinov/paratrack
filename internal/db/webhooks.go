package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// Webhook persistence owns endpoint registration and delivery history.
type Webhook = webhookport.Webhook

type WebhookDelivery = webhookport.WebhookDeliverySummary

// CreateWebhook registers an endpoint.
func (d *DB) CreateWebhook(ctx context.Context, request appmodel.WebhookRegistrationCommand) (webhookport.WebhookSummary, error) {
	teamID, callerID := request.TeamID, request.CallerID
	url, secret, events := strings.TrimSpace(request.URL), request.Secret, request.Events
	if url == "" || (!strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://")) {
		return webhookport.WebhookSummary{}, fmt.Errorf("url must be http(s)")
	}
	if events == "" {
		events = "session.stopped,invoice.created"
	}
	now := FormatTime(d.currentTime().UTC())
	sealedSecret, err := d.sealSecret(secret)
	if err != nil {
		return webhookport.WebhookSummary{}, fmt.Errorf("seal webhook secret: %w", err)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return webhookport.WebhookSummary{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return webhookport.WebhookSummary{}, err
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO webhooks (team_id, url, secret, events, active, created_at)
		 VALUES (?, ?, ?, ?, 1, ?) RETURNING id`, teamID, url, sealedSecret, events, now).Scan(&id)
	if err != nil {
		return webhookport.WebhookSummary{}, err
	}
	webhook, err := scanWebhookSummary(tx.QueryRowContext(ctx,
		`SELECT id, team_id, url, events, active, created_at FROM webhooks WHERE id = ? AND team_id = ?`, id, teamID))
	if err != nil {
		return webhookport.WebhookSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return webhookport.WebhookSummary{}, err
	}
	return webhook, nil
}

// ListWebhooks returns the team's endpoints.
func (d *DB) ListWebhooks(ctx context.Context, query appmodel.WebhookListQuery) ([]Webhook, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, url, secret, events, active, created_at
		 FROM webhooks WHERE team_id = ? ORDER BY id`, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		h, err := d.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListWebhookSummaries returns endpoint settings without loading signing secrets.
func (d *DB) ListWebhookSummaries(ctx context.Context, query appmodel.WebhookListQuery) ([]webhookport.WebhookSummary, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, url, events, active, created_at
		 FROM webhooks WHERE team_id = ? ORDER BY id`, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webhookport.WebhookSummary
	for rows.Next() {
		h, err := scanWebhookSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// GetWebhook fetches one endpoint inside its workspace.
func (d *DB) GetWebhook(ctx context.Context, query appmodel.WebhookLookupQuery) (Webhook, error) {
	if query.TeamID <= 0 || query.WebhookID <= 0 {
		return Webhook{}, ErrNotFound
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, url, secret, events, active, created_at
		 FROM webhooks WHERE id = ? AND team_id = ?`, query.WebhookID, query.TeamID)
	h, err := d.scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	return h, err
}

// DeleteWebhook removes an endpoint and its delivery log.
func (d *DB) DeleteWebhook(ctx context.Context, request appmodel.WebhookDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.WebhookID <= 0 {
		return ErrNotFound
	}
	teamID, callerID, id := request.TeamID, request.CallerID, request.WebhookID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = ? AND team_id = ?`, id, teamID)
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

// LogWebhookDelivery records delivery metadata without retaining event payloads.
// Logging is best effort so an observability failure cannot block delivery.
func (d *DB) LogWebhookDelivery(ctx context.Context, request appmodel.WebhookDeliveryLogRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, status, error, created_at)
		 VALUES (?, ?, '', ?, ?, ?)`,
		request.WebhookID, request.Event, request.Status, request.Error, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return fmt.Errorf("insert webhook delivery log: %w", err)
	}
	return nil
}

// ListWebhookDeliveries returns the last N attempts for an endpoint.
func (d *DB) ListWebhookDeliveries(ctx context.Context, teamID, webhookID int64) ([]WebhookDelivery, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT wd.id, wd.webhook_id, wd.event, wd.status, wd.error, wd.created_at
		 FROM webhook_deliveries wd JOIN webhooks wh ON wh.id = wd.webhook_id
		 WHERE wh.team_id = ? AND wd.webhook_id = ? ORDER BY wd.id DESC LIMIT 20`, teamID, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var w WebhookDelivery
		var created string
		if err := rows.Scan(&w.ID, &w.WebhookID, &w.Event, &w.Status, &w.Error, &created); err != nil {
			return nil, err
		}
		w.CreatedAt, err = ScanTime(created)
		if err != nil {
			return nil, fmt.Errorf("parse webhook delivery time: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListRecentWebhookDeliveries returns at most limit newest attempts for each
// endpoint in one workspace-scoped query.
func (d *DB) ListRecentWebhookDeliveries(ctx context.Context, query appmodel.WebhookDeliveryHistoryQuery) (map[int64][]webhookport.WebhookDeliverySummary, error) {
	if query.TeamID <= 0 || query.Limit <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`WITH ranked AS (
			SELECT wd.id, wd.webhook_id, wd.event, wd.status, wd.error, wd.created_at,
			       ROW_NUMBER() OVER (PARTITION BY wd.webhook_id ORDER BY wd.id DESC) AS position
			FROM webhook_deliveries wd
			JOIN webhooks wh ON wh.id = wd.webhook_id
			WHERE wh.team_id = ?
		)
		SELECT id, webhook_id, event, status, error, created_at
		FROM ranked WHERE position <= ?
		ORDER BY webhook_id, id DESC`, query.TeamID, query.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := make(map[int64][]webhookport.WebhookDeliverySummary)
	for rows.Next() {
		var delivery webhookport.WebhookDeliverySummary
		var createdAt string
		if err := rows.Scan(&delivery.ID, &delivery.WebhookID, &delivery.Event, &delivery.Status, &delivery.Error, &createdAt); err != nil {
			return nil, err
		}
		delivery.CreatedAt, err = ScanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse webhook delivery time: %w", err)
		}
		deliveries[delivery.WebhookID] = append(deliveries[delivery.WebhookID], delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (d *DB) scanWebhook(r interface{ Scan(...any) error }) (Webhook, error) {
	var (
		h       Webhook
		active  int
		created string
	)
	if err := r.Scan(&h.ID, &h.TeamID, &h.URL, &h.Secret, &h.Events, &active, &created); err != nil {
		return Webhook{}, err
	}
	h.Active = active != 0
	h.Secret = d.mustOpen(h.Secret)
	createdAt, err := ScanTime(created)
	if err != nil {
		return Webhook{}, fmt.Errorf("parse webhook creation time: %w", err)
	}
	h.CreatedAt = createdAt
	return h, nil
}

func scanWebhookSummary(r interface{ Scan(...any) error }) (webhookport.WebhookSummary, error) {
	var (
		h       webhookport.WebhookSummary
		active  int
		created string
	)
	if err := r.Scan(&h.ID, &h.TeamID, &h.URL, &h.Events, &active, &created); err != nil {
		return webhookport.WebhookSummary{}, err
	}
	h.Active = active != 0
	createdAt, err := ScanTime(created)
	if err != nil {
		return webhookport.WebhookSummary{}, fmt.Errorf("parse webhook creation time: %w", err)
	}
	h.CreatedAt = createdAt
	return h, nil
}
