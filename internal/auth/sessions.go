package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Session represents a logged-in browser. The token is the only thing
// the browser stores (HttpOnly cookie); everything else lives in the DB.
type Session = model.AuthSession

// SessionTTL is the lifetime we mint for new sessions.
const SessionTTL = appmodel.SessionTTL

// ErrSessionInvalid is returned by findByToken when the token doesn't
// match a live session — either unknown, already expired, or deleted.
var ErrSessionInvalid = appmodel.ErrAuthSessionInvalid

// newSession creates a session for the given user and returns the
// token string. The caller is responsible for putting it into an
// HttpOnly cookie.
func (s *Service) newSession(ctx context.Context, userID int64) (Session, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now().UTC()
	return s.sessions.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
		Token: token, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
	})
}

// findByToken returns the session and user for a live browser token. Expired
// sessions are deleted on the way out so they don't accumulate.
func (s *Service) findByToken(ctx context.Context, token string) (Session, appmodel.UserIdentity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Session{}, appmodel.UserIdentity{}, ErrSessionInvalid
	}
	sess, user, err := s.sessions.FindAuthSession(ctx, token)
	if errors.Is(err, model.ErrNotFound) {
		return Session{}, appmodel.UserIdentity{}, ErrSessionInvalid
	}
	if err != nil {
		return Session{}, appmodel.UserIdentity{}, err
	}
	if expiredAt(sess.ExpiresAt, s.now().UTC()) {
		if err := s.deleteByToken(ctx, token); err != nil {
			s.logger.Printf("auth: delete expired session: %v", err)
		}
		return Session{}, appmodel.UserIdentity{}, ErrSessionInvalid
	}
	return sess, identityOf(user), nil
}

// AuthenticateSessionToken validates a browser session and returns only the
// credential-free identity needed by request middleware. The caller already
// has the presented token and can pass it to Touch separately.
func (s *Service) AuthenticateSessionToken(ctx context.Context, token string) (appmodel.UserIdentity, error) {
	_, user, err := s.findByToken(ctx, token)
	return user, err
}

// Touch bumps last_seen_at. Cheap UPDATE; safe to call on every request.
func (s *Service) Touch(ctx context.Context, token string) {
	if err := s.sessions.TouchAuthSession(ctx, appmodel.AuthSessionTouchRequest{Token: token, At: s.now().UTC()}); err != nil {
		s.logger.Printf("auth: update session last seen: %v", err)
	}
}

// deleteByToken removes a session row. Used by logout and by expiry.
func (s *Service) deleteByToken(ctx context.Context, token string) error {
	return s.sessions.DeleteAuthSession(ctx, appmodel.AuthSessionDeleteRequest{Token: token})
}

// Logout deletes the caller's browser session and records the successful
// user initiated transition. Expiry cleanup continues to use deleteByToken.
func (s *Service) Logout(ctx context.Context, request appmodel.LogoutRequest) error {
	if strings.TrimSpace(request.Token) != "" {
		if err := s.deleteByToken(ctx, request.Token); err != nil {
			return err
		}
	}
	s.recordAudit(ctx, request.TeamID, request.UserID, "auth.logout", "", "")
	return nil
}

// DeleteByUser removes every session for a user. Useful for "log out
// everywhere" or when an account is being closed.
func (s *Service) DeleteByUser(ctx context.Context, userID int64) error {
	return s.sessions.DeleteAuthSessionsByUser(ctx, appmodel.AuthSessionsDeleteByUserRequest{UserID: userID})
}

// PurgeExpired deletes every session whose expires_at has passed. Call
// it on a timer if you want; not run automatically.
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	return s.sessions.PurgeExpiredAuthSessions(ctx, appmodel.AuthSessionsPurgeExpiredRequest{Before: s.now().UTC()})
}
