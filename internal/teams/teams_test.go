package teams

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/testutil"
)

func openTestDB(t *testing.T) *dbpkg.DB {
	t.Helper()
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func newTestService(t *testing.T, d *dbpkg.DB) *Service {
	t.Helper()
	service, err := NewService(Dependencies{
		Teams: d, Memberships: d, Invites: d, Settings: d, Modules: d,
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("create teams service: %v", err)
	}
	return service
}

func newUser(t *testing.T, d *dbpkg.DB, email string) int64 {
	t.Helper()
	// Direct insert — we don't need the auth.Service for these tests.
	var id int64
	err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?) RETURNING id`,
		email, "x", email).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Team!":     "my-team",
		"   spaced   ": "spaced",
		"foo_bar-baz":  "foo-bar-baz",
		"!!!@@":        "team",
		"":             "team",
		"привет":       "privet", // Cyrillic is transliterated
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateCreatesOwnerMembership(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	uid := newUser(t, d, "alice@example.com")

	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: uid, Name: "Alice"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	role, ok, err := svc.IsMember(ctx, team.ID, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("user should be a member of their personal team")
	}
	if role != RoleOwner {
		t.Errorf("personal team role: want owner, got %s", role)
	}
}

func TestAcceptInviteRejectsAtExactExpiryUsingInjectedClock(t *testing.T) {
	d := openTestDB(t)
	clock := time.Date(2025, time.March, 4, 5, 6, 7, 0, time.UTC)
	svc, err := NewService(Dependencies{
		Teams: d, Memberships: d, Invites: d, Settings: d, Modules: d,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("create teams service: %v", err)
	}
	ctx := context.Background()
	owner := newUser(t, d, "invite-clock-owner@example.com")
	member := newUser(t, d, "invite-clock-member@example.com")
	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Clock Workspace"})
	if err != nil {
		t.Fatalf("Create team: %v", err)
	}
	invite, err := svc.NewInvite(ctx, appmodel.TeamInviteCreateRequest{TeamID: team.ID, CallerID: owner})
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	clock = invite.ExpiresAt
	if _, err := svc.AcceptInvite(ctx, appmodel.TeamInviteAcceptRequest{Token: invite.Token, UserID: member}); !errors.Is(err, ErrValidation) {
		t.Fatalf("AcceptInvite at expiry: want validation error, got %v", err)
	}
}

func TestDeleteTeamChecksCurrentOwnerInStore(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	owner := newUser(t, d, "owner-delete@example.com")
	other := newUser(t, d, "other-delete@example.com")
	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Owned Team"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, appmodel.WorkspaceDeleteRequest{TeamID: team.ID, CallerID: other}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("Delete as non-owner: want forbidden, got %v", err)
	}
}

func TestDeleteAndListRemainingReturnsWorkspacesFromDeletion(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	owner := newUser(t, d, "owner-delete-remaining@example.com")
	deleted, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Deleted Workspace"})
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Remaining Workspace"})
	if err != nil {
		t.Fatal(err)
	}

	workspaces, err := svc.DeleteAndListRemaining(ctx, appmodel.WorkspaceDeleteRequest{TeamID: deleted.ID, CallerID: owner})
	if err != nil {
		t.Fatalf("DeleteAndListRemaining: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != remaining.ID {
		t.Fatalf("remaining workspaces = %+v, want only %d", workspaces, remaining.ID)
	}
}

func TestCreateTeamAndList(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	uid := newUser(t, d, "alice@example.com")

	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: uid, Name: "Side Project"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if team.OwnerID != uid {
		t.Errorf("team owner: want %d, got %d", uid, team.OwnerID)
	}
	if team.Slug == "" {
		t.Errorf("team slug should be set")
	}

	teams, err := svc.ListForUser(ctx, uid)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(teams) == 0 {
		t.Errorf("ListForUser should return at least the new team")
	}
}

func TestInviteFlow(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	owner := newUser(t, d, "owner@example.com")
	member := newUser(t, d, "member@example.com")

	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Dev Team"})
	if err != nil {
		t.Fatal(err)
	}

	// Non-owner can't invite.
	if _, err := svc.NewInvite(ctx, appmodel.TeamInviteCreateRequest{TeamID: team.ID, CallerID: member}); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-owner NewInvite: want ErrForbidden, got %v", err)
	}

	inv, err := svc.NewInvite(ctx, appmodel.TeamInviteCreateRequest{TeamID: team.ID, CallerID: owner})
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if inv.Token == "" {
		t.Fatal("invite token should be set")
	}
	if inv.ExpiredAt(inv.CreatedAt) || inv.Used() {
		t.Errorf("new invite should be live at its creation time")
	}

	joined, err := svc.AcceptInvite(ctx, appmodel.TeamInviteAcceptRequest{Token: inv.Token, UserID: member})
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	if joined.ID != team.ID {
		t.Errorf("AcceptInvite returned team %d, want %d", joined.ID, team.ID)
	}

	role, ok, err := svc.IsMember(ctx, team.ID, member)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || role != RoleMember {
		t.Errorf("member should be in team with role=member, got ok=%v role=%v", ok, role)
	}
	createdAt := time.Now().UTC().Add(-2 * time.Hour)
	expired := model.TeamInvite{
		Token: "expired-at-commit", TeamID: team.ID, Role: model.TeamRole(RoleMember),
		CreatedBy: owner, CreatedAt: createdAt, ExpiresAt: createdAt.Add(time.Hour),
	}
	if err := d.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{Token: expired.Token, TeamID: expired.TeamID, Role: expired.Role, CallerID: expired.CreatedBy, CreatedAt: expired.CreatedAt, ExpiresAt: expired.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
	if err := d.AcceptTeamInvite(ctx, appmodel.TeamInviteAcceptanceRequest{Token: expired.Token, TeamID: team.ID, UserID: owner, At: time.Now().UTC()}); !errors.Is(err, model.ErrInviteExpired) {
		t.Errorf("store AcceptTeamInvite after expiry: want expired, got %v", err)
	}
	forged := model.TeamInvite{
		Token: "member-issued", TeamID: team.ID, Role: model.TeamRole(RoleMember),
		CreatedBy: member, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := d.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{Token: forged.Token, TeamID: forged.TeamID, Role: forged.Role, CallerID: forged.CreatedBy, CreatedAt: forged.CreatedAt, ExpiresAt: forged.ExpiresAt}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("store CreateTeamInvite as member: want forbidden, got %v", err)
	}
	if err := d.DeleteTeamInvite(ctx, appmodel.TeamInviteRevokeRequest{TeamID: team.ID, CallerID: member, Token: inv.Token}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("store DeleteTeamInvite as member: want forbidden, got %v", err)
	}
	if err := svc.SetRole(ctx, appmodel.TeamMemberRoleRequest{TeamID: team.ID, TargetUserID: owner, CallerID: member, Role: RoleAdmin}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member SetRole: want ErrForbidden, got %v", err)
	}
	if err := svc.SetRole(ctx, appmodel.TeamMemberRoleRequest{TeamID: team.ID, TargetUserID: member, CallerID: owner, Role: RoleAdmin}); err != nil {
		t.Fatalf("owner SetRole: %v", err)
	}
	role, ok, err = svc.IsMember(ctx, team.ID, member)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || role != RoleAdmin {
		t.Errorf("promoted member role: want admin, got ok=%v role=%v", ok, role)
	}

	// Re-accepting fails (already used).
	if _, err := svc.AcceptInvite(ctx, appmodel.TeamInviteAcceptRequest{Token: inv.Token, UserID: member}); err == nil {
		t.Errorf("second AcceptInvite should fail")
	}
}

func TestRemoveMember_LastOwnerCannotLeave(t *testing.T) {
	d := openTestDB(t)
	svc := newTestService(t, d)
	ctx := context.Background()
	owner := newUser(t, d, "solo@example.com")

	team, err := svc.Create(ctx, appmodel.TeamCreateRequest{OwnerID: owner, Name: "Solo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: team.ID, TargetUserID: owner, CallerID: owner}); err == nil {
		t.Errorf("last owner must not be able to leave (should require deletion)")
	}
}
