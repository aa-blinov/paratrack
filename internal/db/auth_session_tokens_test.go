package db

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestAuthSessionTokenIsStoredAsDigestAndStillRoundTrips(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _, err = d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "session-storage@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "session-token-with-enough-entropy-32bytes"
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	if _, err := d.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
		Token: rawToken, UserID: 1, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT token FROM auth_sessions WHERE token = ?`, authSessionTokenHash(rawToken)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != authSessionTokenHash(rawToken) || stored == rawToken {
		t.Fatalf("stored session token = %q, want the token's digest, never the token", stored)
	}
	var rawRows int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM auth_sessions WHERE token = ?`, rawToken).Scan(&rawRows); err != nil {
		t.Fatal(err)
	}
	if rawRows != 0 {
		t.Fatal("the raw session token is in the database")
	}
	session, _, err := d.FindAuthSession(ctx, rawToken)
	if err != nil || session.Token != rawToken {
		t.Fatalf("find session = (%q, %v), want the raw token round trip", session.Token, err)
	}
	if err := d.TouchAuthSession(ctx, appmodel.AuthSessionTouchRequest{Token: rawToken, At: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteAuthSession(ctx, appmodel.AuthSessionDeleteRequest{Token: rawToken}); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM auth_sessions`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("logout left %d session rows behind", remaining)
	}
}

func TestLegacyAuthSessionTokensWorkUntilTheStartupPassHashesThem(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "session-migration@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	const lazyToken = "legacy-session-token-looked-up-later"
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO auth_sessions (token, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		lazyToken, userID, FormatTime(now), FormatTime(now.Add(30*24*time.Hour)), FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	// A raw row from an older binary still resolves: the lookup matches both
	// forms, so nothing breaks mid-deploy. Reads stay pure, so the row is
	// converted by the startup pass, not on use.
	session, _, err := d.FindAuthSession(ctx, lazyToken)
	if err != nil || session.Token != lazyToken {
		t.Fatalf("legacy lookup = (%q, %v), want the raw token round trip", session.Token, err)
	}
	var rawRows int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM auth_sessions WHERE token = ?`, lazyToken).Scan(&rawRows); err != nil {
		t.Fatal(err)
	}
	if rawRows != 1 {
		t.Fatal("the lookup must not rewrite the row it read")
	}
	// The startup pass hashes what is left; the journal lets it re-run after
	// the row count changed since the previous start.
	if _, err := d.TestSQL().ExecContext(ctx,
		`DELETE FROM paratrack_migrations WHERE name = '20261119_hash_auth_session_tokens'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateAuthSessionTokensContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM auth_sessions WHERE token = ?`, lazyToken).Scan(&rawRows); err != nil {
		t.Fatal(err)
	}
	if rawRows != 0 {
		t.Fatal("the startup pass left a raw session token behind")
	}
	if _, _, err := d.FindAuthSession(ctx, lazyToken); err != nil {
		t.Fatalf("lookup after startup pass: %v", err)
	}
}

func TestPasswordChangeRevokesEveryOtherSession(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "password-revoke@example.com", PasswordHash: "the-old-hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "bystander@example.com", PasswordHash: "hash", Name: "Other", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	for _, token := range []string{"current-browser-session", "stolen-phone-session"} {
		if _, err := d.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
			Token: token, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
		Token: "bystander-session", UserID: otherID, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	actorCtx := requestctx.WithActor(ctx, userID)
	updated, err := d.UpdateUserPasswordIfHashMatches(actorCtx, appmodel.PasswordHashUpdateRequest{
		UserID: userID, CallerID: userID, ExpectedHash: "the-old-hash", PasswordHash: "the-new-hash",
		KeepSessionToken: "current-browser-session",
	})
	if err != nil || !updated {
		t.Fatalf("password change = (%v, %v)", updated, err)
	}
	var kept, stolen, bystander int
	for token, count := range map[string]*int{
		"current-browser-session": &kept, "stolen-phone-session": &stolen, "bystander-session": &bystander,
	} {
		if err := d.TestSQL().QueryRowContext(ctx,
			`SELECT count(*) FROM auth_sessions WHERE token IN (?, ?)`,
			authSessionTokenHash(token), token).Scan(count); err != nil {
			t.Fatal(err)
		}
	}
	if kept != 1 {
		t.Fatal("the session that changed the password was revoked too")
	}
	if stolen != 0 {
		t.Fatal("a stolen session survived the password change")
	}
	if bystander != 1 {
		t.Fatal("the password change touched another account's session")
	}
	// Without a browser session to keep, every session of the account goes.
	if _, err := d.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
		Token: "second-browser-session", UserID: userID, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateAuthSession(ctx, appmodel.AuthSessionCreateRequest{
		Token: "third-browser-session", UserID: userID, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateUserPasswordIfHashMatches(actorCtx, appmodel.PasswordHashUpdateRequest{
		UserID: userID, CallerID: userID, ExpectedHash: "the-new-hash", PasswordHash: "the-newest-hash",
	}); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM auth_sessions WHERE user_id = ?`, userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("a password change with no session to keep left %d sessions alive", remaining)
	}
}
