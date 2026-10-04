package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// ResetTTL is how long a forgot-password link stays valid.
const ResetTTL = appmodel.ResetTTL

// ErrResetInvalid is returned when a token is unknown, expired, or
// already used. Callers must not distinguish — the user gets one
// generic message.
var ErrResetInvalid = appmodel.ErrAuthResetInvalid

// RequestPasswordReset resolves an account and mints a single-use reset
// token. Transport adapters should keep their response identical for
// ErrNotFound and successful requests to avoid account enumeration.
func (s *Service) RequestPasswordReset(ctx context.Context, request appmodel.PasswordResetRequest) (appmodel.UserIdentity, string, error) {
	user, err := s.findByEmail(ctx, request.Email)
	if err != nil {
		return appmodel.UserIdentity{}, "", err
	}
	token, err := s.createPasswordReset(ctx, user.ID)
	if err != nil {
		return appmodel.UserIdentity{}, "", fmt.Errorf("create password reset token: %w", err)
	}
	return identityOf(user), token, nil
}

// createPasswordReset mints a single-use token and atomically invalidates
// previous outstanding tokens for the user.
func (s *Service) createPasswordReset(ctx context.Context, userID int64) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now().UTC()
	if err := s.resets.CreatePasswordReset(ctx, appmodel.PasswordResetCreateRequest{
		TokenHash: passwordResetTokenHash(token), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(ResetTTL),
	}); err != nil {
		return "", err
	}
	return token, nil
}

// consumePasswordReset validates the token, changes the password, marks the
// token used, and revokes existing sessions in one storage transaction.
func (s *Service) consumePasswordReset(ctx context.Context, token, newPassword string) (User, error) {
	if len(newPassword) < 8 || len(newPassword) > 72 {
		return User{}, fmt.Errorf("%w: password must be 8-72 characters", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user, err := s.resets.ConsumePasswordReset(ctx, appmodel.PasswordResetConsumeRequest{
		TokenHash: passwordResetTokenHash(token), LegacyToken: token, PasswordHash: string(hash), At: s.now().UTC(),
	})
	if errors.Is(err, model.ErrNotFound) {
		return User{}, ErrResetInvalid
	}
	if err != nil {
		return User{}, err
	}
	s.recordAudit(ctx, 0, user.ID, "auth.password_reset", user.Email, "")
	return user, nil
}

func passwordResetTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
}

// CompletePasswordReset consumes the reset token and starts a fresh session
// for the account whose previous sessions were revoked by the reset.
func (s *Service) CompletePasswordReset(ctx context.Context, request appmodel.PasswordResetCompletionRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, error) {
	user, err := s.consumePasswordReset(ctx, request.Token, request.NewPassword)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
	}
	session, err := s.newSession(ctx, user.ID)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, fmt.Errorf("create session after password reset: %w", err)
	}
	return identityOf(user), appmodel.AuthSessionCredential{Token: session.Token}, nil
}
