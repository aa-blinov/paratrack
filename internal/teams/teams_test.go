package teams

import (
	"context"
	"errors"
	"testing"

	dbpkg "github.com/aa-blinov/paratrack/internal/db"
)

func openTestDB(t *testing.T) *dbpkg.DB {
	t.Helper()
	d, err := dbpkg.Open(t.TempDir() + "/teams.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func newUser(t *testing.T, d *dbpkg.DB, email string) int64 {
	t.Helper()
	// Direct insert — we don't need the auth.Service for these tests.
	var id int64
	err := d.SQL().QueryRow(
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?) RETURNING id`,
		email, "x", email).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Team!":      "my-team",
		"   spaced   ":  "spaced",
		"foo_bar-baz":   "foo-bar-baz",
		"!!!@@":         "team",
		"":              "team",
		"привет":        "team", // non-ascii stripped
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreatePersonalInTx(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	uid := newUser(t, d, "alice@example.com")

	tx, err := d.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	teamID, err := svc.CreatePersonalInTx(ctx, tx, uid, "Alice")
	if err != nil {
		t.Fatalf("CreatePersonalInTx: %v", err)
	}
	if teamID == 0 {
		t.Fatal("teamID should be set")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	role, ok, err := svc.IsMember(ctx, teamID, uid)
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

func TestCreateTeamAndList(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	uid := newUser(t, d, "alice@example.com")

	team, err := svc.Create(ctx, uid, "Side Project")
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
	svc := NewService(d)
	ctx := context.Background()
	owner := newUser(t, d, "owner@example.com")
	member := newUser(t, d, "member@example.com")

	team, err := svc.Create(ctx, owner, "Dev Team")
	if err != nil {
		t.Fatal(err)
	}

	// Non-owner can't invite.
	if _, err := svc.NewInvite(ctx, team.ID, member); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-owner NewInvite: want ErrForbidden, got %v", err)
	}

	inv, err := svc.NewInvite(ctx, team.ID, owner)
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if inv.Token == "" {
		t.Fatal("invite token should be set")
	}
	if !inv.Live() {
		t.Errorf("new invite should be live")
	}

	joined, err := svc.AcceptInvite(ctx, inv.Token, member)
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

	// Re-accepting fails (already used).
	if _, err := svc.AcceptInvite(ctx, inv.Token, member); err == nil {
		t.Errorf("second AcceptInvite should fail")
	}
}

func TestRemoveMember_LastOwnerCannotLeave(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	owner := newUser(t, d, "solo@example.com")

	team, err := svc.Create(ctx, owner, "Solo")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveMember(ctx, team.ID, owner, owner); err == nil {
		t.Errorf("last owner must not be able to leave (should require deletion)")
	}
}