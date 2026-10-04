package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestTeamInviteTokenIsHashedAtRestAndReadableByManager(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "invite-token-test-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-storage@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "high-entropy-invite-token"
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := d.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{
		Token: rawToken, TeamID: teamID, Role: model.TeamRoleMember, CallerID: ownerID,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	var storedHash, sealed string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT token, sealed_token FROM invites WHERE token = ?`, teamInviteTokenHash(rawToken)).Scan(&storedHash, &sealed); err != nil {
		t.Fatal(err)
	}
	if storedHash != teamInviteTokenHash(rawToken) || storedHash == rawToken {
		t.Fatalf("stored token = %q, want its hash", storedHash)
	}
	if !strings.HasPrefix(sealed, sealedPrefix) || strings.Contains(sealed, rawToken) {
		t.Fatalf("invite token was not encrypted at rest: %q", sealed)
	}
	invite, err := d.FindTeamInvite(ctx, rawToken)
	if err != nil || invite.Token != rawToken {
		t.Fatalf("find invite = (%q, %v), want raw token round trip", invite.Token, err)
	}
	invites, err := d.ListTeamInvites(ctx, teamID)
	if err != nil || len(invites) != 1 || invites[0].Token != rawToken {
		t.Fatalf("list invites = (%+v, %v), want manager-visible token", invites, err)
	}
	d.secrets = newSecretCodec("wrong-invite-token-key")
	if err := d.sealExistingSecretsContext(ctx); err == nil {
		t.Fatal("startup secret validation accepted an encrypted invite token with a wrong key")
	}
}

func TestLegacyTeamInviteTokensAreMigratedWithoutChangingInviteLinks(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "invite-migration-test-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-migration@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "existing-live-invite-token"
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO invites (token, team_id, role, created_by, created_at, expires_at) VALUES (?, ?, 'member', ?, ?, ?)`,
		rawToken, teamID, ownerID, FormatTime(now), FormatTime(now.Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`DELETE FROM paratrack_migrations WHERE name = '20261004_hash_invite_tokens'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateInviteTokensContext(ctx); err != nil {
		t.Fatal(err)
	}
	invite, err := d.FindTeamInvite(ctx, rawToken)
	if err != nil || invite.Token != rawToken {
		t.Fatalf("legacy invite lookup = (%q, %v), want existing link preserved", invite.Token, err)
	}
	var plaintextRows int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM invites WHERE token = ?`, rawToken).Scan(&plaintextRows); err != nil {
		t.Fatal(err)
	}
	if plaintextRows != 0 {
		t.Fatalf("legacy plaintext token remains in the lookup column")
	}
}
