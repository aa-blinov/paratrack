package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// maxChallengeAttempts ends a code step after this many wrong codes.
const maxChallengeAttempts = 5

// verificationResendInterval is the least time between two confirmation links for one account.
const verificationResendInterval = time.Minute

// recoveryCodeCount is how many one-time recovery codes an account gets when it turns the
// second factor on.
const recoveryCodeCount = 10

// twoFactorIssuer is the name an authenticator app lists the account under.
const twoFactorIssuer = "paratrack"

// AccountSecurityStore keeps the confirmation links and the second factor of accounts. The
// secrets are sealed by the store; everything above it works with plain values.
type AccountSecurityStore interface {
	IssueEmailVerification(context.Context, appmodel.EmailVerificationIssueRequest) (bool, error)
	ConsumeEmailVerification(context.Context, appmodel.EmailVerificationConsumeRequest) (model.User, error)
	TwoFactorState(context.Context, int64) (appmodel.TwoFactorState, error)
	SetTwoFactorPending(context.Context, appmodel.TwoFactorPendingRequest) error
	EnableTwoFactor(context.Context, appmodel.TwoFactorEnableRequest) error
	RecordTwoFactorStep(context.Context, appmodel.TwoFactorStepRequest) (bool, error)
	DisableTwoFactor(context.Context, appmodel.TwoFactorUserRequest) error
	CreateTwoFactorChallenge(context.Context, appmodel.TwoFactorChallengeCreateRequest) error
	TwoFactorChallenge(context.Context, string, time.Time) (appmodel.TwoFactorChallenge, error)
	RecordChallengeFailure(context.Context, appmodel.TwoFactorChallengeRequest) (int64, error)
	DeleteTwoFactorChallenge(context.Context, appmodel.TwoFactorChallengeRequest) error
	ConsumeRecoveryCode(context.Context, appmodel.RecoveryCodeConsumeRequest) (bool, error)
}

// tokenDigest is the stored form of every one-time token: a SHA-256 of the value, so a leaked
// table does not hand out working links or codes.
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newOpaqueToken returns 32 random bytes as URL-safe text.
func newOpaqueToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// RegisterAccount creates the account. With the email check on, it starts unverified and
// returns the confirmation token to mail, with no session; without it, it starts a session.
func (s *Service) RegisterAccount(ctx context.Context, request appmodel.RegistrationRequest) (appmodel.RegistrationOutcome, error) {
	if !s.requireMail {
		session, teamID, err := s.RegisterAndStartSession(ctx, request)
		if err != nil {
			return appmodel.RegistrationOutcome{}, err
		}
		return appmodel.RegistrationOutcome{Session: session, TeamID: teamID}, nil
	}
	userID, teamID, err := s.createAccount(ctx, request, true)
	if err != nil {
		return appmodel.RegistrationOutcome{}, err
	}
	token, issued, err := s.issueVerification(ctx, userID, 0)
	if err != nil {
		return appmodel.RegistrationOutcome{}, fmt.Errorf("issue signup confirmation: %w", err)
	}
	if !issued {
		return appmodel.RegistrationOutcome{}, fmt.Errorf("issue signup confirmation: no link was issued for a new account")
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	s.recordAudit(ctx, teamID, userID, "auth.register", email, "awaiting confirmation")
	return appmodel.RegistrationOutcome{
		TeamID:      teamID,
		Identity:    appmodel.UserIdentity{ID: userID, Email: email, Name: strings.TrimSpace(request.Name)},
		VerifyToken: token,
	}, nil
}

// issueVerification mints a confirmation link unless one was issued less than minInterval ago.
// A false answer means no link was minted, and the caller sends nothing.
func (s *Service) issueVerification(ctx context.Context, userID int64, minInterval time.Duration) (string, bool, error) {
	token, err := newOpaqueToken()
	if err != nil {
		return "", false, err
	}
	issued, err := s.security.IssueEmailVerification(ctx, appmodel.EmailVerificationIssueRequest{
		UserID: userID, TokenHash: tokenDigest(token), Now: s.now().UTC(),
		TTL: appmodel.VerificationTTL, MinInterval: minInterval,
	})
	if err != nil || !issued {
		return "", false, err
	}
	return token, true, nil
}

// ResendEmailVerification mints a fresh confirmation link for an account that is still
// unverified, at most once a minute. The token is empty when the minute has not passed yet.
func (s *Service) ResendEmailVerification(ctx context.Context, userID int64) (appmodel.UserIdentity, string, error) {
	user, err := s.findByID(ctx, userID)
	if err != nil {
		return appmodel.UserIdentity{}, "", err
	}
	if !user.EmailUnverified {
		return appmodel.UserIdentity{}, "", nil
	}
	token, _, err := s.issueVerification(ctx, userID, verificationResendInterval)
	if err != nil {
		return appmodel.UserIdentity{}, "", fmt.Errorf("resend confirmation: %w", err)
	}
	return identityOf(user), token, nil
}

// ConfirmEmail spends a confirmation link and returns the account it confirmed.
func (s *Service) ConfirmEmail(ctx context.Context, token string) (appmodel.UserIdentity, error) {
	user, err := s.security.ConsumeEmailVerification(ctx, appmodel.EmailVerificationConsumeRequest{
		TokenHash: tokenDigest(token), At: s.now().UTC(),
	})
	if errors.Is(err, model.ErrNotFound) {
		return appmodel.UserIdentity{}, appmodel.ErrAuthVerificationInvalid
	}
	if err != nil {
		return appmodel.UserIdentity{}, err
	}
	s.recordAudit(ctx, 0, user.ID, "auth.email_verified", user.Email, "")
	return identityOf(user), nil
}

// StartTwoFactor opens the code step of a sign-in whose password was right, and returns the
// token that names it. The caller keeps the token in a cookie of its own.
func (s *Service) StartTwoFactor(ctx context.Context, userID int64) (string, error) {
	token, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	if err := s.security.CreateTwoFactorChallenge(ctx, appmodel.TwoFactorChallengeCreateRequest{
		UserID: userID, TokenHash: tokenDigest(token), Now: s.now().UTC(), TTL: appmodel.ChallengeTTL,
	}); err != nil {
		return "", fmt.Errorf("open two-factor step: %w", err)
	}
	return token, nil
}

// CompleteTwoFactor checks the code of an open step and starts the session. A code that is
// wrong counts against the step; the fifth wrong code ends it.
func (s *Service) CompleteTwoFactor(ctx context.Context, request appmodel.TwoFactorLoginRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, error) {
	digest := tokenDigest(request.Challenge)
	code := request.Code
	now := s.now().UTC()
	open, err := s.security.TwoFactorChallenge(ctx, digest, now)
	if errors.Is(err, model.ErrNotFound) {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, appmodel.ErrAuthChallengeInvalid
	}
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
	}
	if open.Attempts >= maxChallengeAttempts {
		_ = s.security.DeleteTwoFactorChallenge(ctx, appmodel.TwoFactorChallengeRequest{TokenHash: digest})
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, appmodel.ErrAuthChallengeInvalid
	}
	ok, err := s.checkSecondFactor(ctx, open.UserID, code, now)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
	}
	if !ok {
		attempts, err := s.security.RecordChallengeFailure(ctx, appmodel.TwoFactorChallengeRequest{TokenHash: digest})
		if err != nil {
			return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
		}
		if attempts >= maxChallengeAttempts {
			_ = s.security.DeleteTwoFactorChallenge(ctx, appmodel.TwoFactorChallengeRequest{TokenHash: digest})
			return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, appmodel.ErrAuthChallengeInvalid
		}
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, appmodel.ErrAuthTwoFactorInvalid
	}
	if err := s.security.DeleteTwoFactorChallenge(ctx, appmodel.TwoFactorChallengeRequest{TokenHash: digest}); err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
	}
	user, err := s.findByID(ctx, open.UserID)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, err
	}
	session, err := s.newSession(ctx, user.ID)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, fmt.Errorf("create login session: %w", err)
	}
	s.recordAudit(ctx, 0, user.ID, "auth.login", user.Email, "two-factor")
	return identityOf(user), appmodel.AuthSessionCredential{Token: session.Token}, nil
}

// checkSecondFactor accepts a code of the authenticator app, once per step, or one unused
// recovery code of the account.
func (s *Service) checkSecondFactor(ctx context.Context, userID int64, code string, now time.Time) (bool, error) {
	state, err := s.security.TwoFactorState(ctx, userID)
	if err != nil {
		return false, err
	}
	if !state.Enabled {
		return false, nil
	}
	if step, ok := verifyTOTP(state.Secret, code, now); ok {
		return s.security.RecordTwoFactorStep(ctx, appmodel.TwoFactorStepRequest{UserID: userID, Step: step})
	}
	normalized := normalizeRecoveryCode(code)
	if normalized == "" {
		return false, nil
	}
	return s.security.ConsumeRecoveryCode(ctx, appmodel.RecoveryCodeConsumeRequest{
		UserID: userID, CodeHash: tokenDigest(normalized), At: now,
	})
}

// BeginTwoFactor starts the set-up of the second factor. It keeps a new secret as pending;
// nothing changes for the sign-in until a code from the app confirms it.
func (s *Service) BeginTwoFactor(ctx context.Context, userID int64) (appmodel.TwoFactorSetup, error) {
	state, err := s.security.TwoFactorState(ctx, userID)
	if err != nil {
		return appmodel.TwoFactorSetup{}, err
	}
	if state.Enabled {
		return appmodel.TwoFactorSetup{}, appmodel.ErrAuthTwoFactorState
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return appmodel.TwoFactorSetup{}, err
	}
	if err := s.security.SetTwoFactorPending(ctx, appmodel.TwoFactorPendingRequest{UserID: userID, Secret: secret}); err != nil {
		return appmodel.TwoFactorSetup{}, err
	}
	user, err := s.findByID(ctx, userID)
	if err != nil {
		return appmodel.TwoFactorSetup{}, err
	}
	return appmodel.TwoFactorSetup{Secret: secret, URI: otpauthURI(twoFactorIssuer, user.Email, secret)}, nil
}

// TwoFactorEnabled tells whether the second factor is on for the account.
func (s *Service) TwoFactorEnabled(ctx context.Context, userID int64) (bool, error) {
	state, err := s.security.TwoFactorState(ctx, userID)
	if err != nil {
		return false, err
	}
	return state.Enabled, nil
}

// ConfirmTwoFactor turns the second factor on once the app shows a code of the pending secret.
// It returns the recovery codes; they are shown once and stored only as hashes.
func (s *Service) ConfirmTwoFactor(ctx context.Context, request appmodel.TwoFactorConfirmRequest) ([]string, error) {
	userID := request.UserID
	state, err := s.security.TwoFactorState(ctx, userID)
	if err != nil {
		return nil, err
	}
	if state.Enabled || state.Pending == "" {
		return nil, appmodel.ErrAuthTwoFactorState
	}
	step, ok := verifyTOTP(state.Pending, request.Code, s.now().UTC())
	if !ok {
		return nil, appmodel.ErrAuthTwoFactorInvalid
	}
	codes, hashes, err := newRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, err
	}
	if err := s.security.EnableTwoFactor(ctx, appmodel.TwoFactorEnableRequest{
		UserID: userID, Secret: state.Pending, Step: step, CodeHashes: hashes,
	}); err != nil {
		return nil, err
	}
	s.recordAudit(ctx, 0, userID, "auth.two_factor_on", "", "")
	return codes, nil
}

// DisableTwoFactor turns the second factor off. It asks the password and a current code, so
// a stolen session alone cannot remove the protection.
func (s *Service) DisableTwoFactor(ctx context.Context, request appmodel.TwoFactorDisableRequest) error {
	userID := request.UserID
	user, err := s.findByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verifyPassword(user, request.Password); err != nil {
		return appmodel.ErrAuthBadPassword
	}
	state, err := s.security.TwoFactorState(ctx, userID)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return appmodel.ErrAuthTwoFactorState
	}
	ok, err := s.checkSecondFactor(ctx, userID, request.Code, s.now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return appmodel.ErrAuthTwoFactorInvalid
	}
	if err := s.security.DisableTwoFactor(ctx, appmodel.TwoFactorUserRequest{UserID: userID}); err != nil {
		return err
	}
	s.recordAudit(ctx, 0, userID, "auth.two_factor_off", "", "")
	return nil
}

// newRecoveryCodes returns n codes to show once, and the hashes that are stored. A code is
// ten random bytes in base32, shown in four groups of four.
func newRecoveryCodes(n int) ([]string, []string, error) {
	codes := make([]string, 0, n)
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var b [10]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		raw := totpEncoding.EncodeToString(b[:])
		code := raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
		codes = append(codes, code)
		hashes = append(hashes, tokenDigest(normalizeRecoveryCode(code)))
	}
	return codes, hashes, nil
}

// normalizeRecoveryCode reads a typed recovery code: upper case, no dashes or spaces. Any other
// length is not a code and gives the empty string.
func normalizeRecoveryCode(code string) string {
	cleaned := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	if len(cleaned) != 16 {
		return ""
	}
	return cleaned
}
