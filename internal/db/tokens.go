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

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// API tokens — long-lived bearer credentials for the browser extension
// and CLI. The raw token is shown once; only its SHA-256 is stored.
// ---------------------------------------------------------------------------

type APIToken = model.APIToken
type TokenOptions = appmodel.TokenOptions

// ErrTokenInvalid is returned by APITokenByRaw when the bearer is unknown.
var ErrTokenInvalid = model.ErrTokenInvalid

// HashAPIToken returns the hex SHA-256 of the raw token. Tokens are
// high-entropy so a fast hash is fine (unlike passwords).
func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken mints a token and returns the raw secret (shown once)
// plus the stored row.
func (d *DB) CreateAPIToken(ctx context.Context, request appmodel.APITokenCreateRequest) (string, APIToken, error) {
	if request.UserID <= 0 {
		return "", APIToken{}, model.ErrForbidden
	}
	if request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return "", APIToken{}, model.ErrForbidden
	}
	o := request.Options
	if o.TeamID < 0 {
		return "", APIToken{}, model.ErrForbidden
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "api token"
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", APIToken{}, err
	}
	raw := "pt_" + hex.EncodeToString(b[:])
	hash := HashAPIToken(raw)
	var now time.Time
	var id int64
	var exp any
	ro := 0
	if o.ReadOnly {
		ro = 1
	}
	prepare := func() error {
		now = d.currentTime().UTC()
		if o.ExpiresAt == nil {
			return nil
		}
		if !o.ExpiresAt.After(now) {
			return appmodel.ErrAuthValidation
		}
		exp = FormatTime(o.ExpiresAt.UTC())
		return nil
	}
	insert := func(queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}) error {
		return queryer.QueryRowContext(ctx,
			`INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at, team_id, expires_at, read_only)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			request.UserID, name, hash, raw[:11], FormatTime(now), nullableInt64(o.TeamID), exp, ro).Scan(&id)
	}
	var err error
	if o.TeamID > 0 {
		var tx *Tx
		tx, err = d.sql.BeginTx(ctx, nil)
		if err == nil {
			defer func() { _ = tx.Rollback() }()
			if err = lockCurrentTeamMember(ctx, tx, o.TeamID, request.UserID); err == nil {
				err = prepare()
				if err == nil {
					err = insert(tx)
				}
			}
			if err == nil {
				err = tx.Commit()
			}
		}
	} else {
		err = prepare()
		if err == nil {
			err = insert(d.sql)
		}
	}
	if err != nil {
		return "", APIToken{}, err
	}
	return raw, APIToken{ID: id, UserID: request.UserID, Name: name, Prefix: raw[:11], CreatedAt: now,
		TeamID: o.TeamID, ExpiresAt: o.ExpiresAt, ReadOnly: o.ReadOnly}, nil
}

// ListAPITokens returns the user's tokens (never the raw secrets).
func (d *DB) ListAPITokens(ctx context.Context, request appmodel.APITokenListRequest) ([]APIToken, error) {
	if request.UserID <= 0 || request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return nil, model.ErrForbidden
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, user_id, name, prefix, created_at, last_used_at, COALESCE(team_id, 0), expires_at, read_only
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, request.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var (
			t                 APIToken
			created, lastUsed sql.NullString
			expires           sql.NullString
			ro                int
		)
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &created, &lastUsed, &t.TeamID, &expires, &ro); err != nil {
			return nil, err
		}
		t.ReadOnly = ro == 1
		if expires.Valid {
			ts, err := ScanTime(expires.String)
			if err != nil {
				return nil, fmt.Errorf("parse API token expiry: %w", err)
			}
			if ts.IsZero() {
				return nil, fmt.Errorf("parse API token expiry: empty timestamp")
			}
			t.ExpiresAt = &ts
		}
		t.CreatedAt, err = ScanTime(created.String)
		if err != nil {
			return nil, fmt.Errorf("parse API token creation time: %w", err)
		}
		if lastUsed.Valid {
			ts, err := ScanTime(lastUsed.String)
			if err != nil {
				return nil, fmt.Errorf("parse API token last-used time: %w", err)
			}
			t.LastUsedAt = &ts
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken removes one token.
func (d *DB) DeleteAPIToken(ctx context.Context, request appmodel.APITokenDeleteRequest) error {
	if request.UserID <= 0 || request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, request.TokenID, request.UserID)
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
	return nil
}

// APITokenByRaw looks up a bearer token and touches last_used_at.

func (d *DB) APITokenByRaw(ctx context.Context, request appmodel.APITokenLookupRequest) (APIToken, error) {
	raw := request.Raw
	if raw == "" {
		return APIToken{}, ErrTokenInvalid
	}
	hash := HashAPIToken(raw)
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, user_id, name, prefix, created_at, COALESCE(team_id, 0), expires_at, read_only
		 FROM api_tokens WHERE token_hash = ?`, hash)
	var (
		t       APIToken
		created string
		expires sql.NullString
		ro      int
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &created, &t.TeamID, &expires, &ro); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIToken{}, ErrTokenInvalid
		}
		return APIToken{}, err
	}
	createdAt, err := ScanTime(created)
	if err != nil {
		return APIToken{}, fmt.Errorf("parse API token creation time: %w", err)
	}
	t.CreatedAt = createdAt
	t.ReadOnly = ro == 1
	if expires.Valid {
		ts, err := ScanTime(expires.String)
		if err != nil {
			return APIToken{}, fmt.Errorf("parse API token expiry: %w", err)
		}
		if ts.IsZero() {
			return APIToken{}, errors.New("parse API token expiry: empty timestamp")
		}
		if !ts.After(d.currentTime()) {
			return APIToken{}, ErrTokenInvalid // expired
		}
		t.ExpiresAt = &ts
	}
	if _, err := d.sql.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		FormatTime(d.currentTime().UTC()), t.ID); err != nil {
		d.logger.Printf("api tokens: update last-used timestamp: %v", err)
	}
	return t, nil
}
