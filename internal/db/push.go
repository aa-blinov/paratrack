package db

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Wave 9: Web Push subscriptions + VAPID keypair
// ---------------------------------------------------------------------------

// PushSubscription is one browser's push endpoint.
type PushSubscription struct {
	ID       int64  `json:"id"`
	TeamID   int64  `json:"team_id"`
	UserID   int64  `json:"user_id"`
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// UpsertPushSubscription stores (or refreshes) a browser subscription.
func (d *DB) UpsertPushSubscription(ctx context.Context, teamID, userID int64, endpoint, p256dh, auth string) error {
	if endpoint == "" || p256dh == "" || auth == "" {
		return errors.New("endpoint, p256dh and auth are required")
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO push_subscriptions (team_id, user_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET
		  team_id = excluded.team_id,
		  user_id = excluded.user_id,
		  p256dh = excluded.p256dh,
		  auth = excluded.auth`,
		teamID, userID, endpoint, p256dh, auth,
		formatNowUTC())
	return err
}

func formatNowUTC() string {
	return FormatTime(time.Now().UTC())
}

// ListPushSubscriptions returns every subscription in the team.
func (d *DB) ListPushSubscriptions(ctx context.Context, teamID int64) ([]PushSubscription, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, user_id, endpoint, p256dh, auth
		 FROM push_subscriptions WHERE team_id = ?`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.ID, &s.TeamID, &s.UserID, &s.Endpoint, &s.P256DH, &s.Auth); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeletePushSubscription removes one by endpoint (unsubscribed browser).
func (d *DB) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// EnsureVAPIDKeys generates a keypair on first call and stores it.
// Returns (publicBase64URL, privateBase64URL).
func (d *DB) EnsureVAPIDKeys(ctx context.Context) (string, string, error) {
	var pub, priv string
	err := d.sql.QueryRowContext(ctx,
		`SELECT public_key, private_key FROM push_keys WHERE id = 1`).Scan(&pub, &priv)
	if err == nil && pub != "" {
		return pub, priv, nil
	}
	// generate P-256 keypair
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	// uncompressed public key (0x04 || X || Y)
	pubBytes := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	pub = base64.RawURLEncoding.EncodeToString(pubBytes)
	// private key as 32-byte big-endian scalar
	privBytes := make([]byte, 32)
	key.D.FillBytes(privBytes)
	priv = base64.RawURLEncoding.EncodeToString(privBytes)
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO push_keys (id, public_key, private_key) VALUES (1, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET public_key = excluded.public_key, private_key = excluded.private_key`,
		pub, priv)
	return pub, priv, err
}

// parseScalar decodes a base64url P-256 private scalar.
func parseScalar(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}

var _ = elliptic.P256
