package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// Password-reset tokens are single-use, 30-minute, stored hashed-ish
// (the raw token is what we email; the row is the source of truth).

// ResetTTL is how long a forgot-password link stays valid.
const ResetTTL = 30 * time.Minute

// ErrResetInvalid is returned when a token is unknown, expired, or
// already used. Callers must not distinguish — the user gets one
// generic message.
var ErrResetInvalid = errors.New("reset token invalid")

// CreatePasswordReset mints a single-use token for the user and returns
// the raw string to embed in the email. Previous unused tokens for the
// user are invalidated so only the newest link works.
func (s *Service) CreatePasswordReset(ctx context.Context, userID int64) (string, error) {
	// Invalidate any outstanding tokens.
	_, _ = s.d.SQL().ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = ? WHERE user_id = ? AND used_at IS NULL`,
		db.FormatTime(time.Now().UTC()), userID,
	)
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := time.Now().UTC()
	_, err := s.d.SQL().ExecContext(ctx,
		`INSERT INTO password_reset_tokens (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, db.FormatTime(now), db.FormatTime(now.Add(ResetTTL)),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ConsumePasswordReset validates the token and returns the user it
// belongs to. The token is marked used in the same call — a second
// attempt with the same link fails.
func (s *Service) ConsumePasswordReset(ctx context.Context, token, newPassword string) (User, error) {
	row := s.d.SQL().QueryRowContext(ctx, `
		SELECT t.user_id, t.expires_at, t.used_at,
		       u.id, u.email, u.password_hash, u.name, u.created_at, u.updated_at
		FROM password_reset_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token = ?`, token)
	var (
		userID  int64
		exp, used sql.NullString
		user    User
		uct, uupd string
	)
	if err := row.Scan(&userID, &exp, &used, &user.ID, &user.Email, &user.PasswordHash,
		&user.Name, &uct, &uupd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrResetInvalid
		}
		return User{}, err
	}
	if used.Valid {
		return User{}, ErrResetInvalid
	}
	if exp.Valid {
		if t, err := db.ScanTime(exp.String); err == nil && time.Now().UTC().After(t) {
			return User{}, ErrResetInvalid
		}
	}
	user.CreatedAt, _ = db.ScanTime(uct)
	user.UpdatedAt, _ = db.ScanTime(uupd)

	// Mark used first (single-use), then set the password.
	now := db.FormatTime(time.Now().UTC())
	res, err := s.d.SQL().ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = ? WHERE token = ? AND used_at IS NULL`,
		now, token,
	)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrResetInvalid
	}
	if err := s.UpdatePassword(ctx, user.ID, newPassword); err != nil {
		return User{}, err
	}
	// Kick every existing session — password change must log out
	// other devices.
	_ = s.DeleteByUser(ctx, user.ID)
	return user, nil
}
