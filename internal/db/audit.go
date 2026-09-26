package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Wave 4: audit log + webhooks
// ---------------------------------------------------------------------------

// AuditEntry is one security/ops event. Immutable.
type AuditEntry struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	UserID    int64     `json:"user_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Meta      string    `json:"meta"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// Audit records an event. action examples: "auth.login", "project.delete",
// "invoice.create". teamID/userID may be 0 for pre-auth events.
func (d *DB) Audit(ctx context.Context, teamID, userID int64, action, target, meta, ip string) {
	_, _ = d.sql.ExecContext(ctx,
		`INSERT INTO audit_log (team_id, user_id, action, target, meta, ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		teamID, userID, action, target, meta, ip, FormatTime(time.Now().UTC()))
}

// ListAudit returns the team's audit trail, newest first.
func (d *DB) ListAudit(ctx context.Context, teamID int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, user_id, action, target, meta, ip, created_at
		 FROM audit_log WHERE team_id = ? ORDER BY id DESC LIMIT ?`, teamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var created string
		if err := rows.Scan(&e.ID, &e.TeamID, &e.UserID, &e.Action, &e.Target, &e.Meta, &e.IP, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = ScanTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Webhook is an outbound notification endpoint.
type Webhook struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"`
	Events    string    `json:"events"` // comma-separated
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// WebhookDelivery is one attempt to POST an event.
type WebhookDelivery struct {
	ID        int64     `json:"id"`
	WebhookID int64     `json:"webhook_id"`
	Event     string    `json:"event"`
	Payload   string    `json:"payload"`
	Status    int       `json:"status"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateWebhook registers an endpoint.
func (d *DB) CreateWebhook(ctx context.Context, teamID int64, url, secret, events string) (Webhook, error) {
	url = strings.TrimSpace(url)
	if url == "" || (!strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://")) {
		return Webhook{}, fmt.Errorf("url must be http(s)")
	}
	if events == "" {
		events = "session.stopped,invoice.created"
	}
	now := FormatTime(time.Now().UTC())
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO webhooks (team_id, url, secret, events, active, created_at)
		 VALUES (?, ?, ?, ?, 1, ?)`, teamID, url, secret, events, now)
	if err != nil {
		return Webhook{}, err
	}
	id, _ := res.LastInsertId()
	return d.GetWebhook(ctx, teamID, id)
}

// ListWebhooks returns the team's endpoints.
func (d *DB) ListWebhooks(ctx context.Context, teamID int64) ([]Webhook, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, url, secret, events, active, created_at
		 FROM webhooks WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// GetWebhook fetches one endpoint.
func (d *DB) GetWebhook(ctx context.Context, teamID, id int64) (Webhook, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, url, secret, events, active, created_at
		 FROM webhooks WHERE id = ? AND team_id = ?`, id, teamID)
	h, err := scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	return h, err
}

// DeleteWebhook removes an endpoint and its delivery log.
func (d *DB) DeleteWebhook(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LogWebhookDelivery records an attempt (fire-and-forget from the caller).
func (d *DB) LogWebhookDelivery(ctx context.Context, webhookID int64, event, payload string, status int, errMsg string) {
	_, _ = d.sql.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, status, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		webhookID, event, payload, status, errMsg, FormatTime(time.Now().UTC()))
}

// ListWebhookDeliveries returns the last N attempts for an endpoint.
func (d *DB) ListWebhookDeliveries(ctx context.Context, webhookID int64) ([]WebhookDelivery, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, webhook_id, event, payload, status, error, created_at
		 FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT 20`, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var w WebhookDelivery
		var created string
		if err := rows.Scan(&w.ID, &w.WebhookID, &w.Event, &w.Payload, &w.Status, &w.Error, &created); err != nil {
			return nil, err
		}
		w.CreatedAt, _ = ScanTime(created)
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWebhook(r interface{ Scan(...any) error }) (Webhook, error) {
	var (
		h       Webhook
		active  int
		created string
	)
	if err := r.Scan(&h.ID, &h.TeamID, &h.URL, &h.Secret, &h.Events, &active, &created); err != nil {
		return Webhook{}, err
	}
	h.Active = active != 0
	h.CreatedAt, _ = ScanTime(created)
	return h, nil
}
