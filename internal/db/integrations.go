package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// API tokens — long-lived bearer credentials for the browser extension
// and CLI. The raw token is shown once; only its SHA-256 is stored.
// ---------------------------------------------------------------------------

// APIToken is a named credential owned by a user.
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"` // first 8 chars, for the tokens list
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// ErrTokenInvalid is returned by APITokenByRaw when the bearer is unknown.
var ErrTokenInvalid = errors.New("api token invalid")

// HashAPIToken returns the hex SHA-256 of the raw token. Tokens are
// high-entropy so a fast hash is fine (unlike passwords).
func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken mints a token and returns the raw secret (shown once)
// plus the stored row.
func (d *DB) CreateAPIToken(ctx context.Context, userID int64, name string) (string, APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "api token"
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", APIToken{}, err
	}
	raw := "pt_" + hex.EncodeToString(b[:])
	hash := HashAPIToken(raw)
	now := time.Now().UTC()
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, name, hash, raw[:11], FormatTime(now)).Scan(&id)
	if err != nil {
		return "", APIToken{}, err
	}
	return raw, APIToken{ID: id, UserID: userID, Name: name, Prefix: raw[:11], CreatedAt: now}, nil
}

// ListAPITokens returns the user's tokens (never the raw secrets).
func (d *DB) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, user_id, name, prefix, created_at, last_used_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var (
			t                            APIToken
			created, lastUsed            sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &created, &lastUsed); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = ScanTime(created.String)
		if lastUsed.Valid {
			if ts, err := ScanTime(lastUsed.String); err == nil {
				t.LastUsedAt = &ts
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken removes one token.
func (d *DB) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// APITokenByRaw looks up a bearer token and touches last_used_at.
func (d *DB) APITokenByRaw(ctx context.Context, raw string) (APIToken, error) {
	if raw == "" {
		return APIToken{}, ErrTokenInvalid
	}
	hash := HashAPIToken(raw)
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, user_id, name, prefix, created_at FROM api_tokens WHERE token_hash = ?`, hash)
	var (
		t       APIToken
		created string
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIToken{}, ErrTokenInvalid
		}
		return APIToken{}, err
	}
	t.CreatedAt, _ = ScanTime(created)
	_, _ = d.sql.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		FormatTime(time.Now().UTC()), t.ID)
	return t, nil
}

// ---------------------------------------------------------------------------
// Integrations (github / trello)
// ---------------------------------------------------------------------------

// Integration is one connected third-party account.
type Integration struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Provider  string    `json:"provider"` // github | trello
	Name      string    `json:"name"`
	Secret    string    `json:"-"` // API token / key — never serialise
	Config    string    `json:"config"`
	CreatedAt time.Time `json:"created_at"`
}

// ExternalTask is a GitHub issue / Trello card imported into paratrack.
type ExternalTask struct {
	ID            int64  `json:"id"`
	IntegrationID int64  `json:"integration_id"`
	ExternalID    string `json:"external_id"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Status        string `json:"status"`
	ActivityID    int64  `json:"activity_id"`
}

// CreateIntegration stores a connection. name is the display label
// (e.g. "acme/api-server" or "Product board").
func (d *DB) CreateIntegration(ctx context.Context, teamID int64, provider, name, secret, config string) (Integration, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Integration{}, fmt.Errorf("name is required")
	}
	now := FormatTime(time.Now().UTC())
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO integrations (team_id, provider, name, secret, config, created_at)
		 VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, provider, name, secret, config, now).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return Integration{}, ErrDuplicate
		}
		return Integration{}, err
	}
	return d.GetIntegration(ctx, teamID, id)
}

// ListIntegrations returns the team's connections (secret included for
// server-side API calls only — templates must not print it).
func (d *DB) ListIntegrations(ctx context.Context, teamID int64) ([]Integration, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, provider, name, secret, config, created_at
		 FROM integrations WHERE team_id = ? ORDER BY provider, name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Integration
	for rows.Next() {
		var (
			it      Integration
			created string
		)
		if err := rows.Scan(&it.ID, &it.TeamID, &it.Provider, &it.Name, &it.Secret, &it.Config, &created); err != nil {
			return nil, err
		}
		it.CreatedAt, _ = ScanTime(created)
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetIntegration fetches one connection inside a team.
func (d *DB) GetIntegration(ctx context.Context, teamID, id int64) (Integration, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, provider, name, secret, config, created_at
		 FROM integrations WHERE id = ? AND team_id = ?`, id, teamID)
	var (
		it      Integration
		created string
	)
	if err := row.Scan(&it.ID, &it.TeamID, &it.Provider, &it.Name, &it.Secret, &it.Config, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, err
	}
	it.CreatedAt, _ = ScanTime(created)
	return it, nil
}

// DeleteIntegration removes the connection and its imported tasks.
func (d *DB) DeleteIntegration(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM integrations WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertExternalTask imports or refreshes one issue / card.
func (d *DB) UpsertExternalTask(ctx context.Context, integrationID int64, externalID, title, url, status string) (ExternalTask, error) {
	now := FormatTime(time.Now().UTC())
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO external_tasks (integration_id, external_id, title, url, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (integration_id, external_id) DO UPDATE SET
		  title = excluded.title,
		  url = excluded.url,
		  status = excluded.status`,
		integrationID, externalID, title, url, status, now)
	if err != nil {
		return ExternalTask{}, err
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, integration_id, external_id, title, url, status, COALESCE(activity_id, 0)
		 FROM external_tasks WHERE integration_id = ? AND external_id = ?`,
		integrationID, externalID)
	return scanExternalTask(row)
}

// ListExternalTasks returns imported items for an integration.
func (d *DB) ListExternalTasks(ctx context.Context, integrationID int64) ([]ExternalTask, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, integration_id, external_id, title, url, status, COALESCE(activity_id, 0)
		 FROM external_tasks WHERE integration_id = ? ORDER BY status, title`, integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalTask
	for rows.Next() {
		t, err := scanExternalTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanExternalTask(r interface{ Scan(...any) error }) (ExternalTask, error) {
	var t ExternalTask
	if err := r.Scan(&t.ID, &t.IntegrationID, &t.ExternalID, &t.Title, &t.URL, &t.Status, &t.ActivityID); err != nil {
		return ExternalTask{}, err
	}
	return t, nil
}
