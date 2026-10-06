// Package auth owns the user / session / password machinery behind
// the collaboration layer.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"golang.org/x/crypto/bcrypt"
)

// User preserves the auth package API while sharing the persistence record.
type User = model.User

// UserStore provides account creation and profile operations.
type UserStore interface {
	CreateAccount(context.Context, appmodel.AccountCreateRequest) (int64, int64, error)
	FindUserByEmail(context.Context, string) (model.User, error)
	FindUserByID(context.Context, int64) (model.User, error)
	FindUserIdentitiesByIDs(context.Context, []int64) (map[int64]appmodel.UserIdentity, error)
	UpdateUserName(context.Context, appmodel.ProfileNameRequest) error
	UpdateUserEmail(context.Context, appmodel.ProfileEmailUpdateRequest) (bool, error)
	UpdateUserPasswordIfHashMatches(context.Context, appmodel.PasswordHashUpdateRequest) (bool, error)
}

// SessionStore provides authentication-session persistence.
type SessionStore interface {
	CreateAuthSession(context.Context, appmodel.AuthSessionCreateRequest) (model.AuthSession, error)
	FindAuthSession(context.Context, string) (model.AuthSession, model.User, error)
	TouchAuthSession(context.Context, appmodel.AuthSessionTouchRequest) error
	DeleteAuthSession(context.Context, appmodel.AuthSessionDeleteRequest) error
	DeleteAuthSessionsByUser(context.Context, appmodel.AuthSessionsDeleteByUserRequest) error
	PurgeExpiredAuthSessions(context.Context, appmodel.AuthSessionsPurgeExpiredRequest) (int64, error)
}

// PasswordResetStore provides single-use password-reset persistence.
type PasswordResetStore interface {
	CreatePasswordReset(context.Context, appmodel.PasswordResetCreateRequest) error
	ConsumePasswordReset(context.Context, appmodel.PasswordResetConsumeRequest) (model.User, error)
}

// APITokenStore provides API-token lifecycle operations.
type APITokenStore interface {
	CreateAPIToken(context.Context, appmodel.APITokenCreateRequest) (string, model.APIToken, error)
	ListAPITokens(context.Context, appmodel.APITokenListRequest) ([]model.APIToken, error)
	DeleteAPIToken(context.Context, appmodel.APITokenDeleteRequest) error
	APITokenByRaw(context.Context, appmodel.APITokenLookupRequest) (model.APIToken, error)
}

// MembershipReader provides the membership check required to scope tokens.
type MembershipReader interface {
	FindMembershipForUser(context.Context, appmodel.TeamMembershipQuery) (model.TeamMembership, bool, error)
}

// Dependencies binds each authentication workflow to a narrow persistence port.
type Dependencies struct {
	Users       UserStore
	Sessions    SessionStore
	Resets      PasswordResetStore
	Tokens      APITokenStore
	Memberships MembershipReader
	Now         func() time.Time
	Audit       AuditRecorder
	Logger      Logger
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
	RecordGlobal(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

var ErrIncompleteDependencies = errors.New("authentication service dependencies are incomplete")

// Service groups authentication workflows. Construct one with NewService
// and reuse it across handlers.
type Service struct {
	users       UserStore
	sessions    SessionStore
	resets      PasswordResetStore
	tokens      APITokenStore
	memberships MembershipReader
	now         func() time.Time
	audit       AuditRecorder
	logger      Logger
}

// NewService returns a Service bound to its persistence contract.
func NewService(deps Dependencies) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"users", deps.Users}, {"sessions", deps.Sessions}, {"password resets", deps.Resets},
		{"API tokens", deps.Tokens}, {"memberships", deps.Memberships},
		{"audit recorder", deps.Audit}, {"logger", deps.Logger},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	if deps.Now == nil {
		return nil, fmt.Errorf("%w: clock", ErrIncompleteDependencies)
	}
	return &Service{
		users: deps.Users, sessions: deps.Sessions, resets: deps.Resets,
		tokens: deps.Tokens, memberships: deps.Memberships, now: deps.Now,
		audit: deps.Audit, logger: deps.Logger,
	}, nil
}

// ErrInvalidEmail is returned when the supplied email is malformed or
// empty. Other invalid cases (password too short, name empty) map to
// the generic ErrValidation.
var (
	ErrInvalidEmail       = appmodel.ErrAuthInvalidEmail
	ErrValidation         = appmodel.ErrAuthValidation
	ErrNotFound           = appmodel.ErrAuthNotFound
	ErrBadPassword        = appmodel.ErrAuthBadPassword
	ErrCredentialsInvalid = appmodel.ErrAuthCredentialsInvalid
	ErrForbidden          = appmodel.ErrAuthForbidden
)

// Validate runs static checks on email / password / name before any DB
// touch. Returns ErrInvalidEmail / ErrValidation with a wrapped reason.
func Validate(email, password, name string) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(email)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEmail, err)
	}
	if len(password) < 8 {
		return fmt.Errorf("%w: password must be at least 8 characters", ErrValidation)
	}
	if len(password) > 72 {
		// bcrypt has a 72-byte input cap; longer passwords are silently
		// truncated, which is surprising. Refuse them up front.
		return fmt.Errorf("%w: password must be at most 72 characters", ErrValidation)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	return nil
}

// createUser inserts a user, hashes the password, and (in one tx) creates
// their personal team + owner row so the new account can immediately
// log in and have an empty workspace. Returns the new id and the
// personal team id (so the caller can set it as the current team).
func (s *Service) createUser(ctx context.Context, email, password, name string) (userID, teamID int64, err error) {
	return s.createUserWithTeamName(ctx, appmodel.RegistrationRequest{
		Email: email, Password: password, Name: name,
		TeamName: strings.TrimSpace(name) + "'s workspace",
	})
}

// createUserWithTeamName creates the account, its personal workspace with the
// caller-selected display name, and owner membership as one store operation.
func (s *Service) createUserWithTeamName(ctx context.Context, request appmodel.RegistrationRequest) (userID, teamID int64, err error) {
	if err := Validate(request.Email, request.Password, request.Name); err != nil {
		return 0, 0, err
	}
	request.TeamName = strings.TrimSpace(request.TeamName)
	if request.TeamName == "" {
		return 0, 0, fmt.Errorf("%w: workspace name is required", ErrValidation)
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.Name = strings.TrimSpace(request.Name)

	hash, err := hashPassword(request.Password)
	if err != nil {
		return 0, 0, fmt.Errorf("hash password: %w", err)
	}

	userID, teamID, err = s.users.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: request.Email, PasswordHash: string(hash), Name: request.Name, TeamName: request.TeamName,
	})
	if err != nil {
		return 0, 0, err
	}
	return userID, teamID, nil
}

// RegisterAndStartSession creates an account with its personal workspace and
// issues the initial session. Validation and account-creation errors retain
// their types for adapter-specific responses.
func (s *Service) RegisterAndStartSession(ctx context.Context, request appmodel.RegistrationRequest) (appmodel.AuthSessionCredential, int64, error) {
	userID, teamID, err := s.createUserWithTeamName(ctx, request)
	if err != nil {
		return appmodel.AuthSessionCredential{}, 0, err
	}
	session, err := s.newSession(ctx, userID)
	if err != nil {
		return appmodel.AuthSessionCredential{}, 0, fmt.Errorf("create registration session: %w", err)
	}
	s.recordAudit(ctx, teamID, userID, "auth.register", strings.ToLower(strings.TrimSpace(request.Email)), "")
	return appmodel.AuthSessionCredential{Token: session.Token}, teamID, nil
}

// findByEmail returns the user with the given email (case-insensitive).
func (s *Service) findByEmail(ctx context.Context, email string) (User, error) {
	user, err := s.users.FindUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	return user, mapStoreNotFound(err)
}

// AuthenticatePassword verifies an email/password pair and starts a session.
// Unknown users and wrong passwords have the same public error.
func (s *Service) AuthenticatePassword(ctx context.Context, request appmodel.PasswordLoginRequest) (appmodel.UserIdentity, appmodel.AuthSessionCredential, error) {
	user, err := s.findByEmail(ctx, request.Email)
	if errors.Is(err, ErrNotFound) {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, ErrCredentialsInvalid
	}
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, fmt.Errorf("find login account: %w", err)
	}
	if err := s.verifyPassword(user, request.Password); err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, ErrCredentialsInvalid
	}
	session, err := s.newSession(ctx, user.ID)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, fmt.Errorf("create login session: %w", err)
	}
	s.recordAudit(ctx, requestctx.TeamID(ctx), user.ID, "auth.login", user.Email, "")
	return identityOf(user), appmodel.AuthSessionCredential{Token: session.Token}, nil
}

func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	var err error
	if teamID == 0 {
		err = s.audit.RecordGlobal(effectCtx, model.AuditRecord{
			UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
		})
	} else {
		err = s.audit.Record(effectCtx, model.AuditRecord{
			TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
		})
	}
	if err != nil {
		s.logger.Printf("auth: record %s audit for user %d: %v", action, actorID, err)
	}
}

// AuthenticateSSO resolves the account for a provider-verified email,
// creating its personal workspace on first use, then issues an auth session.
// If callbacks race to register the same address, the losing callback reloads
// and uses the account that won.
func (s *Service) AuthenticateSSO(ctx context.Context, request appmodel.SSOAuthenticationRequest) (identity appmodel.UserIdentity, session appmodel.AuthSessionCredential, created bool, err error) {
	var user User
	user, err = s.findByEmail(ctx, request.Email)
	if errors.Is(err, ErrNotFound) {
		request.Name = strings.TrimSpace(request.Name)
		if request.Name == "" {
			request.Name = strings.SplitN(strings.TrimSpace(request.Email), "@", 2)[0]
		}
		passwordBytes := make([]byte, 32)
		if _, err := rand.Read(passwordBytes); err != nil {
			return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, false, fmt.Errorf("generate SSO password: %w", err)
		}
		password := hex.EncodeToString(passwordBytes)
		userID, _, createErr := s.createUserWithTeamName(ctx, appmodel.RegistrationRequest{
			Email: request.Email, Password: password, Name: request.Name, TeamName: request.TeamName,
		})
		if createErr != nil {
			// A concurrent callback may have inserted the same normalized email.
			// Resolve that unique-key race without weakening ordinary registration.
			user, err = s.findByEmail(ctx, request.Email)
			if err != nil {
				return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, false, fmt.Errorf("create SSO account: %w", createErr)
			}
		} else {
			user, err = s.findByID(ctx, userID)
			if err != nil {
				return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, false, fmt.Errorf("load created SSO account: %w", err)
			}
			created = true
		}
	} else if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, false, fmt.Errorf("find SSO account: %w", err)
	}
	createdSession, err := s.newSession(ctx, user.ID)
	if err != nil {
		return appmodel.UserIdentity{}, appmodel.AuthSessionCredential{}, false, fmt.Errorf("create SSO session: %w", err)
	}
	session = appmodel.AuthSessionCredential{Token: createdSession.Token}
	if created {
		s.recordAudit(ctx, requestctx.TeamID(ctx), user.ID, "auth.sso_register", user.Email, request.Subject)
	}
	s.recordAudit(ctx, requestctx.TeamID(ctx), user.ID, "auth.sso_login", user.Email, request.Subject)
	return identityOf(user), session, created, nil
}

// findByID returns the user with the given id.
func (s *Service) findByID(ctx context.Context, id int64) (User, error) {
	user, err := s.users.FindUserByID(ctx, id)
	return user, mapStoreNotFound(err)
}

// IdentityByID returns credential-free fields for transport and notification
// use cases that need only an account's display identity.
func (s *Service) IdentityByID(ctx context.Context, id int64) (appmodel.UserIdentity, error) {
	user, err := s.findByID(ctx, id)
	if err != nil {
		return appmodel.UserIdentity{}, err
	}
	return identityOf(user), nil
}

// FindIdentitiesByID returns credential-free identities for the requested IDs.
// Missing IDs are omitted from the result.
func (s *Service) FindIdentitiesByID(ctx context.Context, ids []int64) (map[int64]appmodel.UserIdentity, error) {
	identities, err := s.users.FindUserIdentitiesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("find user identities by IDs: %w", err)
	}
	return identities, nil
}

// verifyPassword returns nil if the supplied password matches the
// user's stored hash, or ErrBadPassword otherwise.
func (s *Service) verifyPassword(user User, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrBadPassword
	}
	return nil
}

// UpdateName changes the user's display name. Empty names are rejected.
func (s *Service) UpdateName(ctx context.Context, request appmodel.ProfileNameRequest) error {
	if request.UserID <= 0 {
		return ErrNotFound
	}
	if request.CallerID <= 0 || request.CallerID != request.UserID {
		return ErrForbidden
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrValidation)
	}
	return mapStoreNotFound(s.users.UpdateUserName(ctx, request))
}

// UpdatePassword re-hashes the new password. Same length rules as
// registration (8-72 chars).
// ChangePassword verifies the caller's current credential and replaces it
// without exposing the stored password hash to transport adapters. The
// conditional write prevents a concurrent credential change from being
// overwritten with a hash verified against an older value.
func (s *Service) ChangePassword(ctx context.Context, request appmodel.PasswordChangeRequest) error {
	userID, currentPassword, newPassword := request.UserID, request.CurrentPassword, request.NewPassword
	if request.CallerID <= 0 || request.CallerID != userID {
		return ErrForbidden
	}
	user, err := s.findByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verifyPassword(user, currentPassword); err != nil {
		return err
	}
	if len(newPassword) < 8 || len(newPassword) > 72 {
		return fmt.Errorf("%w: password must be 8-72 characters", ErrValidation)
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	updated, err := s.users.UpdateUserPasswordIfHashMatches(ctx, appmodel.PasswordHashUpdateRequest{
		UserID: userID, CallerID: request.CallerID, ExpectedHash: user.PasswordHash, PasswordHash: string(hash),
	})
	if err != nil {
		return mapStoreNotFound(err)
	}
	if !updated {
		return ErrBadPassword
	}
	s.recordAudit(ctx, requestctx.TeamID(ctx), userID, "auth.password_change", user.Email, "")
	return nil
}

// ChangeEmail moves the account to a new login address. Like ChangePassword
// it requires the current credential: a live session alone must not be enough
// to hand the account's login to another mailbox. Existing sessions survive,
// because whoever holds the session already proved the password.
func (s *Service) ChangeEmail(ctx context.Context, request appmodel.ProfileEmailRequest) error {
	if request.UserID <= 0 {
		return ErrNotFound
	}
	if request.CallerID <= 0 || request.CallerID != request.UserID {
		return ErrForbidden
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(request.Email)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEmail, err)
	}
	newEmail := strings.ToLower(strings.TrimSpace(request.Email))
	user, err := s.findByID(ctx, request.UserID)
	if err != nil {
		return err
	}
	if err := s.verifyPassword(user, request.CurrentPassword); err != nil {
		return err
	}
	if newEmail == user.Email {
		// Nothing to do. Reporting this as success keeps the form from
		// claiming a change that did not happen.
		return nil
	}
	updated, err := s.users.UpdateUserEmail(ctx, appmodel.ProfileEmailUpdateRequest{
		UserID: user.ID, CallerID: request.CallerID, ExpectedEmail: user.Email, Email: newEmail,
	})
	if errors.Is(err, appmodel.ErrAuthEmailTaken) {
		return appmodel.ErrAuthEmailTaken
	}
	if err != nil {
		return mapStoreNotFound(err)
	}
	if !updated {
		// Someone changed the address between the read and the write; refuse
		// instead of overwriting whatever they set.
		return ErrBadPassword
	}
	s.recordAudit(ctx, requestctx.TeamID(ctx), user.ID, "auth.email_change", user.Email, newEmail)
	return nil
}

func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

func identityOf(user User) appmodel.UserIdentity {
	return appmodel.UserIdentity{ID: user.ID, Email: user.Email, Name: user.Name}
}

func mapStoreNotFound(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
