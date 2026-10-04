package db

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/pushport"
)

// ---------------------------------------------------------------------------
// Push subscriptions and VAPID keypair.
// ---------------------------------------------------------------------------

// UpsertPushSubscription stores (or refreshes) a browser subscription.
func (d *DB) UpsertPushSubscription(ctx context.Context, request appmodel.PushSubscribeRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	if request.Endpoint == "" || request.PublicKey == "" || request.AuthSecret == "" {
		return errors.New("endpoint, p256dh and auth are required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var lockedID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, request.TeamID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, request.UserID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, request.TeamID, request.UserID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO push_subscriptions (team_id, user_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET
		  team_id = excluded.team_id,
		  user_id = excluded.user_id,
		  p256dh = excluded.p256dh,
		  auth = excluded.auth
		WHERE push_subscriptions.user_id = excluded.user_id`,
		request.TeamID, request.UserID, request.Endpoint, request.PublicKey, request.AuthSecret,
		d.formatNowUTC())
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("push subscription belongs to another user")
	}
	return tx.Commit()
}

func (d *DB) formatNowUTC() string {
	return FormatTime(d.currentTime().UTC())
}

// ListPushSubscriptions returns every subscription in the team.
func (d *DB) ListPushSubscriptions(ctx context.Context, teamID int64, userIDs ...int64) ([]pushport.Subscription, error) {
	q := `SELECT id, team_id, user_id, endpoint, p256dh, auth FROM push_subscriptions WHERE team_id = ?`
	args := []any{teamID}
	if len(userIDs) > 0 {
		q += ` AND user_id = ANY(?)`
		args = append(args, userIDs)
	}
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pushport.Subscription
	for rows.Next() {
		var s pushport.Subscription
		if err := rows.Scan(&s.ID, &s.TeamID, &s.UserID, &s.Endpoint, &s.P256DH, &s.Auth); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountPushSubscriptions returns a workspace's device count without loading
// endpoint or browser key material.
func (d *DB) CountPushSubscriptions(ctx context.Context, teamID int64) (int, error) {
	if teamID <= 0 {
		return 0, model.ErrNotFound
	}
	var count int
	err := d.sql.QueryRowContext(ctx,
		`SELECT count(*) FROM push_subscriptions WHERE team_id = ?`, teamID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// DeletePushSubscription removes one of the caller's endpoints. Endpoint
// alone is not an authorization boundary.
func (d *DB) DeletePushSubscription(ctx context.Context, request appmodel.PushUnsubscribeRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	if request.Endpoint == "" {
		return ErrNotFound
	}
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE team_id = ? AND user_id = ? AND endpoint = ?`, request.TeamID, request.UserID, request.Endpoint)
	return err
}

func (d *DB) DeletePushSubscriptionForCleanup(ctx context.Context, request appmodel.PushSubscriptionCleanupRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.Endpoint == "" {
		return ErrNotFound
	}
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE team_id = ? AND user_id = ? AND endpoint = ?`, request.TeamID, request.UserID, request.Endpoint)
	return err
}

// EnsureVAPIDKeys generates a keypair on first call and stores it.
// Returns (publicBase64URL, privateBase64URL).
func (d *DB) EnsureVAPIDKeys(ctx context.Context) (string, string, error) {
	var pub, priv string
	err := d.sql.QueryRowContext(ctx,
		`SELECT public_key, private_key FROM push_keys WHERE id = 1`).Scan(&pub, &priv)
	if err == nil {
		if pub == "" || priv == "" {
			return "", "", errors.New("stored VAPID keypair is incomplete")
		}
		return pub, priv, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("read VAPID keypair: %w", err)
	}
	// generate P-256 keypair
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	// Preserve the Web Push uncompressed point and raw scalar encodings.
	pubBytes, err := key.PublicKey.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("encode VAPID public key: %w", err)
	}
	pub = base64.RawURLEncoding.EncodeToString(pubBytes)
	privBytes, err := key.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("encode VAPID private key: %w", err)
	}
	priv = base64.RawURLEncoding.EncodeToString(privBytes)
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO push_keys (id, public_key, private_key) VALUES (1, ?, ?)
		 ON CONFLICT (id) DO NOTHING`,
		pub, priv)
	if err != nil {
		return "", "", fmt.Errorf("store VAPID keypair: %w", err)
	}
	// Another process may have inserted its generated candidate first. Return
	// the persisted winner so every server instance uses the same keypair.
	if err := d.sql.QueryRowContext(ctx,
		`SELECT public_key, private_key FROM push_keys WHERE id = 1`).Scan(&pub, &priv); err != nil {
		return "", "", fmt.Errorf("read initialized VAPID keypair: %w", err)
	}
	if pub == "" || priv == "" {
		return "", "", errors.New("initialized VAPID keypair is incomplete")
	}
	return pub, priv, nil
}
