package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/audit"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/testutil"
)

// openTestDB creates a temp DB, applies the schema, and returns it
// along with a cleanup. The DB lives in a private file so concurrent
// tests don't share WAL state.
func openTestDB(t *testing.T) *dbpkg.DB {
	t.Helper()
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func newTestAuthService(t *testing.T, d *dbpkg.DB) *Service {
	t.Helper()
	auditService, err := audit.New(d)
	if err != nil {
		t.Fatalf("create audit service: %v", err)
	}
	service, err := NewService(Dependencies{
		Users: d, Sessions: d, Resets: d, Tokens: d, Memberships: d,
		Now: time.Now, Audit: auditService, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	return service
}

func TestValidate(t *testing.T) {
	for _, ok := range []struct{ email, pw, name string }{
		{"alice@example.com", "longenough", "Alice"},
		{"bob@host", "longenough", "Bob"},
	} {
		if err := Validate(ok.email, ok.pw, ok.name); err != nil {
			t.Errorf("Validate(%q,…) should pass: %v", ok.email, err)
		}
	}
	for _, bad := range []struct {
		email, pw, name, why string
	}{
		{"", "longenough", "X", "empty email"},
		{"no-at-sign", "longenough", "X", "malformed email"},
		{"x@y.z", "short", "X", "short password"},
		{"x@y.z", strings.Repeat("a", 73), "X", "long password"},
		{"x@y.z", "longenough", "  ", "empty name"},
	} {
		if err := Validate(bad.email, bad.pw, bad.name); err == nil {
			t.Errorf("%s: Validate should fail", bad.why)
		}
	}
}

func TestCreateUser_HashesPasswordAndCreatesPersonalTeam(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()

	uid, teamID, err := svc.createUser(ctx, "alice@example.com", "longenough", "Alice")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if uid == 0 || teamID == 0 {
		t.Fatalf("ids should be set: uid=%d team=%d", uid, teamID)
	}

	u, err := svc.findByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("findByEmail (mixed case): %v", err)
	}
	if u.ID != uid {
		t.Errorf("findByEmail returned %d, want %d", u.ID, uid)
	}
	if u.PasswordHash == "" || u.PasswordHash == "longenough" {
		t.Errorf("password should be hashed, got %q", u.PasswordHash)
	}

	if err := svc.verifyPassword(u, "longenough"); err != nil {
		t.Errorf("verifyPassword should accept correct password: %v", err)
	}
	if err := svc.verifyPassword(u, "wrong-password"); !errors.Is(err, ErrBadPassword) {
		t.Errorf("verifyPassword wrong-password: want ErrBadPassword, got %v", err)
	}
}

func TestCreateUser_DuplicateEmailRejected(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()

	if _, _, err := svc.createUser(ctx, "dup@example.com", "longenough", "Dup"); err != nil {
		t.Fatalf("first createUser: %v", err)
	}
	if _, _, err := svc.createUser(ctx, "dup@example.com", "longenough", "Dup"); err == nil {
		t.Fatalf("second createUser should fail")
	}
}

func TestChangePasswordUsesCurrentCredentialAndConditionalUpdate(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "password-change@example.com", "old-password", "Password Change")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	ctx = requestctx.WithActor(ctx, userID)

	if err := svc.ChangePassword(ctx, appmodel.PasswordChangeRequest{UserID: userID, CallerID: userID, CurrentPassword: "wrong-password", NewPassword: "new-password"}); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("ChangePassword with wrong current password: got %v, want ErrBadPassword", err)
	}
	user, err := svc.findByID(ctx, userID)
	if err != nil || svc.verifyPassword(user, "old-password") != nil {
		t.Fatalf("wrong current password changed the credential: user=%+v err=%v", user, err)
	}

	staleHash := user.PasswordHash
	if err := svc.ChangePassword(ctx, appmodel.PasswordChangeRequest{UserID: userID, CallerID: userID, CurrentPassword: "old-password", NewPassword: "new-password"}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	var passwordChangeAudits int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = 'auth.password_change'`, userID,
	).Scan(&passwordChangeAudits); err != nil {
		t.Fatalf("count password-change audits: %v", err)
	}
	if passwordChangeAudits != 1 {
		t.Fatalf("password-change audits = %d, want 1", passwordChangeAudits)
	}
	concurrentHash, err := hashPassword("concurrent-password")
	if err != nil {
		t.Fatalf("hash concurrent password: %v", err)
	}
	updated, err := d.UpdateUserPasswordIfHashMatches(ctx, appmodel.PasswordHashUpdateRequest{
		UserID: userID, CallerID: userID, ExpectedHash: staleHash, PasswordHash: string(concurrentHash),
	})
	if err != nil {
		t.Fatalf("conditional stale password update: %v", err)
	}
	if updated {
		t.Fatal("stale hash unexpectedly matched after password change")
	}
	user, err = svc.findByID(ctx, userID)
	if err != nil || svc.verifyPassword(user, "new-password") != nil {
		t.Fatalf("stale update replaced the new credential: user=%+v err=%v", user, err)
	}
}

func TestAuthenticateSSOCreatesOnceAndReusesExistingAccount(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()

	first, firstSession, created, err := svc.AuthenticateSSO(ctx, appmodel.SSOAuthenticationRequest{
		Email: "sso@example.com", Name: "SSO User", TeamName: "SSO Workspace", Subject: "subject-1",
	})
	if err != nil {
		t.Fatalf("first AuthenticateSSO: %v", err)
	}
	if !created {
		t.Fatal("first AuthenticateSSO created=false, want true")
	}
	if firstSession.Token == "" {
		t.Fatal("first AuthenticateSSO returned an empty session token")
	}
	second, secondSession, created, err := svc.AuthenticateSSO(ctx, appmodel.SSOAuthenticationRequest{
		Email: "SSO@example.com", Name: "Changed Name", TeamName: "Changed Workspace", Subject: "subject-1",
	})
	if err != nil {
		t.Fatalf("second AuthenticateSSO: %v", err)
	}
	if created {
		t.Fatal("second AuthenticateSSO created=true, want false")
	}
	if second.ID != first.ID || second.Name != first.Name {
		t.Fatalf("second AuthenticateSSO returned %+v, want existing account %+v", second, first)
	}
	if secondSession.Token == "" || secondSession.Token == firstSession.Token {
		t.Fatal("second AuthenticateSSO did not issue a distinct session")
	}
}

func TestSessions(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	uid, _, err := svc.createUser(ctx, "bob@example.com", "longenough", "Bob")
	if err != nil {
		t.Fatal(err)
	}

	sess, err := svc.newSession(ctx, uid)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	if len(sess.Token) < 20 {
		t.Errorf("token should be reasonably long, got %d chars", len(sess.Token))
	}
	if time.Until(sess.ExpiresAt) < 24*time.Hour {
		t.Errorf("session expiry too close: %v", sess.ExpiresAt)
	}

	got, user, err := svc.findByToken(ctx, sess.Token)
	if err != nil {
		t.Fatalf("findByToken: %v", err)
	}
	if got.UserID != uid || user.ID != uid {
		t.Errorf("session/user id mismatch")
	}

	if _, _, err := svc.findByToken(ctx, "no-such-token"); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("findByToken with junk: want ErrSessionInvalid, got %v", err)
	}

	if err := svc.deleteByToken(ctx, sess.Token); err != nil {
		t.Fatalf("deleteByToken: %v", err)
	}
	if _, _, err := svc.findByToken(ctx, sess.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("after delete: want ErrSessionInvalid, got %v", err)
	}
}

func TestFindByTokenExpiresAtExactBoundary(t *testing.T) {
	d := openTestDB(t)
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	auditService, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Dependencies{
		Users: d, Sessions: d, Resets: d, Tokens: d, Memberships: d,
		Now: func() time.Time { return now }, Audit: auditService, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	userID, _, err := svc.createUser(context.Background(), "boundary@example.com", "longenough", "Boundary")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	session, err := svc.newSession(context.Background(), userID)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	now = now.Add(SessionTTL)
	if _, _, err := svc.findByToken(context.Background(), session.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("findByToken at expiry: want ErrSessionInvalid, got %v", err)
	}
}

func TestAPITokenMutationsAreAuditedWithoutExposingRawToken(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, teamID, err := svc.createUser(ctx, "token-audit@example.com", "longenough", "Token Audit")
	if err != nil {
		t.Fatal(err)
	}
	ctx = requestctx.WithTeamID(requestctx.WithActor(ctx, userID), teamID)
	raw, token, err := svc.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: userID, CallerID: userID, Name: "automation", Options: appmodel.TokenOptions{TeamID: teamID}})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := d.ListAudit(ctx, teamID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "auth.api_token_create" || entries[0].Target != fmt.Sprint(token.ID) || entries[0].Meta != "automation" || strings.Contains(entries[0].Meta, raw) {
		t.Fatalf("token creation audit = %+v", entries)
	}
	if err := svc.DeleteAPIToken(ctx, appmodel.APITokenDeleteRequest{UserID: userID, CallerID: userID, TokenID: token.ID}); err != nil {
		t.Fatal(err)
	}
	entries, err = d.ListAudit(ctx, teamID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Action != "auth.api_token_delete" || entries[0].Target != fmt.Sprint(token.ID) {
		t.Fatalf("token deletion audit = %+v", entries)
	}
}

func TestRegisterAndStartSessionAuditsTheCreatedWorkspace(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	session, teamID, err := svc.RegisterAndStartSession(ctx, appmodel.RegistrationRequest{
		Email: "New.User@example.com", Password: "longenough", Name: "New User", TeamName: "New Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Token == "" || teamID <= 0 {
		t.Fatalf("registration result session=%+v team=%d", session, teamID)
	}
	entries, err := d.ListAudit(ctx, teamID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "auth.register" || entries[0].Target != "new.user@example.com" {
		t.Fatalf("registration audit = %+v", entries)
	}
}

func TestPasswordResetWorkflowIssuesFreshSessionAndRevokesOldOne(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "reset-flow@example.com", "oldpassword", "Reset User")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	oldSession, err := svc.newSession(ctx, userID)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	user, token, err := svc.RequestPasswordReset(ctx, appmodel.PasswordResetRequest{Email: "RESET-flow@example.com"})
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if user.ID != userID || token == "" {
		t.Fatalf("RequestPasswordReset returned user=%+v tokenEmpty=%t", user, token == "")
	}
	var storedToken string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT token FROM password_reset_tokens WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`, userID,
	).Scan(&storedToken); err != nil {
		t.Fatalf("load persisted reset token: %v", err)
	}
	if storedToken == token || !strings.HasPrefix(storedToken, "sha256:") {
		t.Fatalf("persisted reset token is not hashed: %q", storedToken)
	}
	resetUser, newSession, err := svc.CompletePasswordReset(ctx, appmodel.PasswordResetCompletionRequest{Token: token, NewPassword: "newpassword"})
	if err != nil {
		t.Fatalf("CompletePasswordReset: %v", err)
	}
	if resetUser.ID != userID || newSession.Token == "" {
		t.Fatalf("CompletePasswordReset returned user=%+v sessionEmpty=%t", resetUser, newSession.Token == "")
	}
	var resetAudits int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = 'auth.password_reset'`, userID,
	).Scan(&resetAudits); err != nil {
		t.Fatalf("count password-reset audits: %v", err)
	}
	if resetAudits != 1 {
		t.Fatalf("password-reset audits = %d, want 1", resetAudits)
	}
	if _, _, err := svc.findByToken(ctx, oldSession.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old session after reset: want invalid, got %v", err)
	}
	if _, _, err := svc.findByToken(ctx, newSession.Token); err != nil {
		t.Fatalf("new session after reset: %v", err)
	}
	updated, err := svc.findByEmail(ctx, user.Email)
	if err != nil || svc.verifyPassword(updated, "newpassword") != nil {
		t.Fatalf("password was not reset: user=%+v err=%v", updated, err)
	}
}

func TestPasswordResetConsumesLegacyPlaintextToken(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "legacy-reset@example.com", "oldpassword", "Legacy User")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	legacyToken := "legacy-reset-token"
	now := time.Now().UTC()
	if err := d.CreatePasswordReset(ctx, appmodel.PasswordResetCreateRequest{
		TokenHash: legacyToken, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(ResetTTL),
	}); err != nil {
		t.Fatalf("createPasswordReset: %v", err)
	}
	if _, err := svc.consumePasswordReset(ctx, legacyToken, "newpassword"); err != nil {
		t.Fatalf("consume legacy reset token: %v", err)
	}
	var storedToken string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT token FROM password_reset_tokens WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`, userID,
	).Scan(&storedToken); err != nil {
		t.Fatalf("load migrated reset token: %v", err)
	}
	if storedToken == legacyToken || !strings.HasPrefix(storedToken, "sha256:") {
		t.Fatalf("legacy token was not replaced with its digest: %q", storedToken)
	}
}

func TestSessionExpiredIsRejected(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	uid, _, err := svc.createUser(ctx, "carol@example.com", "longenough", "Carol")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.newSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	// Force the session to be expired by rewriting expires_at directly.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE auth_sessions SET expires_at = ? WHERE token = ?`,
		dbpkg.FormatTime(time.Now().UTC().Add(-time.Hour)), sess.Token,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.findByToken(ctx, sess.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("expired session: want ErrSessionInvalid, got %v", err)
	}
}
