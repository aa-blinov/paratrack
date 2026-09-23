package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// Session represents a logged-in browser. The token is the only thing
// the browser stores (HttpOnly cookie); everything else lives in the
// DB.
type Session struct {
	Token       string    `json:"token"`
	UserID      int64     `json:"user_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// SessionTTL is the lifetime we mint for new sessions.
const SessionTTL = 30 * 24 * time.Hour

// CookieName is the cookie key we use. Public so the middleware and
// the logout handler agree.
const CookieName = "paratrack_session"

// ErrSessionInvalid is returned by FindByToken when the token doesn't
// match a live session — either unknown, already expired, or deleted.
var ErrSessionInvalid = errors.New("session invalid")

// NewSession creates a session for the given user and returns the
// token string. The caller is responsible for putting it into an
// HttpOnly cookie.
func (s *Service) NewSession(ctx context.Context, userID int64) (Session, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := time.Now().UTC()
	expires := now.Add(SessionTTL)

	_, err := s.d.SQL().ExecContext(ctx,
		`INSERT INTO auth_sessions (token, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		token, userID, db.FormatTime(now), db.FormatTime(expires), db.FormatTime(now),
	)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, UserID: userID, CreatedAt: now, ExpiresAt: expires, LastSeenAt: now}, nil
}

// FindByToken looks up a session by its token and returns the matching
// User. Expired sessions are deleted on the way out so they don't
// accumulate.
func (s *Service) FindByToken(ctx context.Context, token string) (Session, User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Session{}, User{}, ErrSessionInvalid
	}
	row := s.d.SQL().QueryRowContext(ctx, `
		SELECT s.token, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		       u.id, u.email, u.password_hash, u.name, u.created_at, u.updated_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, token)

	var (
		sess   Session
		user   User
		sct, sexp, slast, uct, uupd string
	)
	if err := row.Scan(
		&sess.Token, &sess.UserID, &sct, &sexp, &slast,
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &uct, &uupd,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, User{}, ErrSessionInvalid
		}
		return Session{}, User{}, err
	}
	sess.CreatedAt, _ = db.ScanTime(sct)
	sess.ExpiresAt, _ = db.ScanTime(sexp)
	sess.LastSeenAt, _ = db.ScanTime(slast)
	user.CreatedAt, _ = db.ScanTime(uct)
	user.UpdatedAt, _ = db.ScanTime(uupd)

	if time.Now().UTC().After(sess.ExpiresAt) {
		_ = s.DeleteByToken(ctx, token)
		return Session{}, User{}, ErrSessionInvalid
	}
	return sess, user, nil
}

// Touch bumps last_seen_at. Cheap UPDATE; safe to call on every request.
func (s *Service) Touch(ctx context.Context, token string) {
	_, _ = s.d.SQL().ExecContext(ctx,
		`UPDATE auth_sessions SET last_seen_at = ? WHERE token = ?`,
		db.FormatTime(time.Now().UTC()), token,
	)
}

// DeleteByToken removes a session row. Used by logout and by expiry.
func (s *Service) DeleteByToken(ctx context.Context, token string) error {
	_, err := s.d.SQL().ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE token = ?`, token,
	)
	return err
}

// DeleteByUser removes every session for a user. Useful for "log out
// everywhere" or when an account is being closed.
func (s *Service) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := s.d.SQL().ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE user_id = ?`, userID,
	)
	return err
}

// PurgeExpired deletes every session whose expires_at has passed. Call
// it on a timer if you want; not run automatically.
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := s.d.SQL().ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE expires_at < ?`,
		db.FormatTime(time.Now().UTC()),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}