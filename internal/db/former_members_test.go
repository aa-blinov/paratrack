package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Someone who left can be brought back as a member, with the pay they left with, and
// the departure row is gone so the pay is counted once (ADM-04). Only a manager can
// bring them back, and someone still in the workspace is refused.
func TestFormerMemberComesBackWithTheirPay(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Возврат", "former-member-return")
	ownerID := teamOwner(t, d, teamID)
	leftID := seedProjectTestUser(t, d, "former-left")
	plainID := seedProjectTestUser(t, d, "former-plain")
	addMember(t, d, teamID, leftID, "member")
	addMember(t, d, teamID, plainID, "member")
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 4200 WHERE team_id = ? AND user_id = ?`, teamID, leftID); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: leftID, CallerID: ownerID, LeftAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	former, err := d.ListFormerMembers(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(former) != 1 || former[0].UserID != leftID {
		t.Fatalf("former members = %+v, want the one who left", former)
	}

	request := appmodel.TeamMemberReturnRequest{TeamID: teamID, TargetUserID: leftID, CallerID: plainID}
	if err := d.ReturnTeamMember(ctx, request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a plain member brought someone back: %v", err)
	}

	request.CallerID = ownerID
	if err := d.ReturnTeamMember(ctx, request); err != nil {
		t.Fatal(err)
	}
	var role string
	var pay int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT role, hourly_pay_cents FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, leftID).Scan(&role, &pay); err != nil {
		t.Fatalf("the returned member has no membership: %v", err)
	}
	if role != "member" || pay != 4200 {
		t.Fatalf("returned as %q at %d cents, want member at 4200", role, pay)
	}
	if former, err := d.ListFormerMembers(ctx, teamID); err != nil || len(former) != 0 {
		t.Fatalf("the returned member still lists as former: %+v (err %v)", former, err)
	}
	if trail := trailOf(t, d, teamID, "member.return"); len(trail) != 1 {
		t.Fatalf("the return wrote %d trail rows, want 1", len(trail))
	}

	if err := d.ReturnTeamMember(ctx, request); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a second return of the same person: %v, want not found", err)
	}
}

// A person who re-joined through an invite while a departure row was left behind is
// still in the workspace: they are not listed as former, and they cannot be returned.
func TestCurrentMemberIsNotListedAsFormerNorReturned(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Текущий", "former-member-current")
	ownerID := teamOwner(t, d, teamID)
	userID := seedProjectTestUser(t, d, "former-current")
	addMember(t, d, teamID, userID, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO former_members (team_id, user_id, hourly_pay_cents, left_at) VALUES (?, ?, 0, ?)`,
		teamID, userID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	former, err := d.ListFormerMembers(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(former) != 0 {
		t.Fatalf("a current member is listed as former: %+v", former)
	}
	err = d.ReturnTeamMember(ctx, appmodel.TeamMemberReturnRequest{TeamID: teamID, TargetUserID: userID, CallerID: ownerID})
	if !errors.Is(err, model.ErrAlreadyExists) {
		t.Fatalf("a current member was returned: %v, want already exists", err)
	}
}
