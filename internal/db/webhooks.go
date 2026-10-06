package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// Webhook persistence owns endpoint registration and delivery history.
type Webhook = webhookport.Webhook

type WebhookDelivery = webhookport.WebhookDeliverySummary

// deliveryBodyLimit caps each stored delivery body. Settings screens show the
// text, and a chatty receiver must not turn one history row into an archive.
const deliveryBodyLimit = 2000

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

// GetWebhookForDelivery loads one endpoint with its signing secret. The caller's
// manager role is re-checked inside the transaction because the result is a
// credential, not just settings.
func (d *DB) GetWebhookForDelivery(ctx context.Context, request appmodel.WebhookLookupCommand) (webhookport.Webhook, error) {
	teamID, callerID, id := request.TeamID, request.CallerID, request.WebhookID
	if teamID <= 0 || callerID <= 0 || id <= 0 {
		return webhookport.Webhook{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return webhookport.Webhook{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return webhookport.Webhook{}, err
	}
	hook, err := d.scanWebhook(tx.QueryRowContext(ctx,
		`SELECT id, team_id, url, secret, events, active, created_at FROM webhooks WHERE id = ? AND team_id = ?`, id, teamID))
	if errors.Is(err, sql.ErrNoRows) {
		return webhookport.Webhook{}, ErrNotFound
	}
	if err != nil {
		return webhookport.Webhook{}, err
	}
	if err := tx.Commit(); err != nil {
		return webhookport.Webhook{}, err
	}
	return hook, nil
}

// LogWebhookDelivery records one attempt with the request and response bodies
// kept for debugging. Logging is best effort so an observability failure cannot
// block delivery.
func (d *DB) LogWebhookDelivery(ctx context.Context, request appmodel.WebhookDeliveryLogRequest) error {
	requestBody, requestTruncated := truncateWebhookBody(request.RequestBody)
	now := FormatTime(d.currentTime().UTC())
	if d.hasDeliveryBodyColumns(ctx) {
		responseBody, responseTruncated := truncateWebhookBody(request.ResponseBody)
		if _, err := d.sql.ExecContext(ctx,
			`INSERT INTO webhook_deliveries (webhook_id, event, payload, request_truncated, response, response_truncated, status, error, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			request.WebhookID, request.Event, requestBody, webhookFlag(requestTruncated), responseBody, webhookFlag(responseTruncated),
			request.Status, request.Error, now); err != nil {
			return fmt.Errorf("insert webhook delivery log: %w", err)
		}
		return nil
	}
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, status, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		request.WebhookID, request.Event, requestBody, request.Status, request.Error, now); err != nil {
		return fmt.Errorf("insert webhook delivery log: %w", err)
	}
	return nil
}

// hasDeliveryBodyColumns reports whether webhook_deliveries carries the columns
// that keep the receiver's answer. The history statements read a database that
// may not have them yet during a rolling deploy, and the columns appear as soon
// as the migration lands, so this asks the catalog instead of caching a schema
// shape for the life of the process. The lookup is a single indexed catalog row
// next to an insert the delivery worker already pays for.
func (d *DB) hasDeliveryBodyColumns(ctx context.Context) bool {
	var count int
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'webhook_deliveries'
		  AND column_name IN ('request_truncated', 'response', 'response_truncated')`).Scan(&count); err != nil {
		return false
	}
	return count == 3
}

// deliveryBodyProjection keeps one scan shape for both table shapes: without the
// response columns the history still reads the request body it has always had,
// just without an answer to show. The aliases let the same column names appear
// in a wrapping CTE.
func (d *DB) deliveryBodyProjection(ctx context.Context, alias string) string {
	projection := fmt.Sprintf("%s.payload AS request_body", alias)
	if !d.hasDeliveryBodyColumns(ctx) {
		return projection + ", 0 AS request_truncated, '' AS response, 0 AS response_truncated"
	}
	return projection + fmt.Sprintf(", %s.request_truncated AS request_truncated, %s.response AS response, %s.response_truncated AS response_truncated", alias, alias, alias)
}

func webhookFlag(value bool) int {
	if value {
		return 1
	}
	return 0
}

// truncateWebhookBody keeps a readable prefix of a stored body. Delivery bodies
// are operational diagnostics, not an archive: a chatty receiver must not grow
// one history row without bound.
func truncateWebhookBody(body string) (string, bool) {
	if len(body) <= deliveryBodyLimit {
		return body, false
	}
	cut := deliveryBodyLimit
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut], true
}

// ListWebhookDeliveries returns the last N attempts for an endpoint.
func (d *DB) ListWebhookDeliveries(ctx context.Context, teamID, webhookID int64) ([]WebhookDelivery, error) {
	bodies := d.deliveryBodyProjection(ctx, "wd")
	rows, err := d.sql.QueryContext(ctx,
		`SELECT wd.id, wd.webhook_id, wd.event, wd.status, wd.error, wd.created_at, `+bodies+`
		 FROM webhook_deliveries wd JOIN webhooks wh ON wh.id = wd.webhook_id
		 WHERE wh.team_id = ? AND wd.webhook_id = ? ORDER BY wd.id DESC LIMIT 20`, teamID, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		w, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
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
	bodies := d.deliveryBodyProjection(ctx, "wd")
	rows, err := d.sql.QueryContext(ctx,
		`WITH ranked AS (
			SELECT wd.id, wd.webhook_id, wd.event, wd.status, wd.error, wd.created_at, `+bodies+`,
			       ROW_NUMBER() OVER (PARTITION BY wd.webhook_id ORDER BY wd.id DESC) AS position
			FROM webhook_deliveries wd
			JOIN webhooks wh ON wh.id = wd.webhook_id
			WHERE wh.team_id = ?
		)
		SELECT id, webhook_id, event, status, error, created_at, request_body, request_truncated, response, response_truncated
		FROM ranked WHERE position <= ?
		ORDER BY webhook_id, id DESC`, query.TeamID, query.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := make(map[int64][]webhookport.WebhookDeliverySummary)
	for rows.Next() {
		var (
			delivery    webhookport.WebhookDeliverySummary
			requestCut  int
			responseCut int
			createdAt   string
		)
		if err := rows.Scan(&delivery.ID, &delivery.WebhookID, &delivery.Event, &delivery.Status, &delivery.Error, &createdAt,
			&delivery.RequestBody, &requestCut, &delivery.ResponseBody, &responseCut); err != nil {
			return nil, err
		}
		delivery.CreatedAt, err = ScanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse webhook delivery time: %w", err)
		}
		delivery.BodyTruncated = requestCut != 0 || responseCut != 0
		deliveries[delivery.WebhookID] = append(deliveries[delivery.WebhookID], delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

// scanWebhookDelivery reads one history row including the stored request body,
// which the older table shape leaves empty.
func scanWebhookDelivery(r interface{ Scan(...any) error }) (WebhookDelivery, error) {
	var (
		w           WebhookDelivery
		created     string
		requestCut  int
		responseCut int
	)
	if err := r.Scan(&w.ID, &w.WebhookID, &w.Event, &w.Status, &w.Error, &created,
		&w.RequestBody, &requestCut, &w.ResponseBody, &responseCut); err != nil {
		return WebhookDelivery{}, err
	}
	w.BodyTruncated = requestCut != 0 || responseCut != 0
	createdAt, err := ScanTime(created)
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("parse webhook delivery time: %w", err)
	}
	w.CreatedAt = createdAt
	return w, nil
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
