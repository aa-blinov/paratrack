package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Removing a spent invitation is the cleanup path behind the "убрать из
// списка" button, so it has to erase the row without touching the membership
// that acceptance created.
func TestRemovingSpentInviteLeavesTheGrantedMembershipIntact(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "invite-removal-test-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-removal-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	inviteeID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-removal-invitee@example.com", PasswordHash: "hash", Name: "Invitee", TeamName: "Personal",
	})
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "spent-invite-token-for-removal"
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := d.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{
		Token: rawToken, TeamID: teamID, Role: model.TeamRoleMember, CallerID: ownerID,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.AcceptTeamInvite(ctx, appmodel.TeamInviteAcceptanceRequest{
		Token: rawToken, TeamID: teamID, UserID: inviteeID, At: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteTeamInvite(ctx, appmodel.TeamInviteRevokeRequest{
		TeamID: teamID, CallerID: ownerID, Token: rawToken,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FindTeamInvite(ctx, rawToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find removed invite = %v, want not found", err)
	}
	invites, err := d.ListTeamInvites(ctx, teamID)
	if err != nil || len(invites) != 0 {
		t.Fatalf("list invites after removal = (%+v, %v), want none", invites, err)
	}
	role, isMember, err := d.TeamMemberRole(ctx, appmodel.TeamMembershipQuery{TeamID: teamID, UserID: inviteeID})
	if err != nil || !isMember {
		t.Fatalf("member lookup after invite removal = (%v, %v), want the invitee still a member", isMember, err)
	}
	if role != model.TeamRoleMember {
		t.Fatalf("member role after invite removal = %q, want %q", role, model.TeamRoleMember)
	}
}

// Cleanup reaches invitations that were never live for the caller, so the
// permission check has to hold for a plain member too.
func TestPlainMemberCannotRemoveAnInviteFromTheTeam(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "invite-removal-permissions-test-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-permission-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	plainID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "invite-permission-plain@example.com", PasswordHash: "hash", Name: "Plain", TeamName: "Personal",
	})
	if err != nil {
		t.Fatal(err)
	}
	const rawToken = "invite-token-guarded-by-permissions"
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := d.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{
		Token: rawToken, TeamID: teamID, Role: model.TeamRoleMember, CallerID: ownerID,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, plainID, FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteTeamInvite(ctx, appmodel.TeamInviteRevokeRequest{
		TeamID: teamID, CallerID: plainID, Token: rawToken,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("delete invite as plain member = %v, want forbidden", err)
	}
	if _, err := d.FindTeamInvite(ctx, rawToken); err != nil {
		t.Fatalf("invite was removed by an unauthorized member: %v", err)
	}
}
