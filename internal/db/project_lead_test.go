package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The lead of a project plans that project and nothing else; a member who is
// not a lead, or who has left the workspace, is refused.
func TestProjectLeadPlansOnlyTheirProject(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	ownerID := seedApprovalUser(t, d, "lead-owner@test.local")
	leadID := seedApprovalUser(t, d, "lead-member@test.local")
	otherID := seedApprovalUser(t, d, "lead-other@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "lead-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for _, id := range []int64{leadID, otherID} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
			teamID, id, FormatTime(time.Now().UTC())); err != nil {
			t.Fatalf("join member: %v", err)
		}
	}
	led, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{
		TeamID: teamID, CallerID: ownerID, Name: "Сайт", Slug: "", Color: "#7c3aed",
		Deadline: "2026-11-30", LeadUserID: leadID,
	})
	if err != nil {
		t.Fatalf("create led project: %v", err)
	}
	if led.LeadUserID != leadID || led.Deadline != "2026-11-30" {
		t.Fatalf("project lead %d and deadline %q not stored", led.LeadUserID, led.Deadline)
	}
	other, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Бот", Slug: "", Color: "#16a34a"})
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}

	cell := func(actor, userID, projectID int64) error {
		return d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{
			TeamID: teamID, ActorID: actor, UserID: userID, ProjectID: projectID,
			Day: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Minutes: 60,
		})
	}
	if err := cell(leadID, otherID, led.ID); err != nil {
		t.Fatalf("lead plans their own project: %v", err)
	}
	if err := cell(leadID, otherID, other.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("lead plans someone else's project = %v, want ErrForbidden", err)
	}
	if err := cell(otherID, otherID, led.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member who leads nothing plans = %v, want ErrForbidden", err)
	}

	// Once the lead leaves the workspace, the lead keeps no say over the plan.
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, leadID); err != nil {
		t.Fatalf("remove lead: %v", err)
	}
	if err := cell(leadID, otherID, led.ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("former lead plans = %v, want ErrForbidden", err)
	}
}

// A deadline must be a real calendar day, and the lead must be a member.
func TestProjectDeadlineAndLeadAreChecked(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "deadline-owner@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "deadline-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Плохой срок", Slug: "", Color: "#7c3aed", Deadline: "2026-02-31"}); err == nil {
		t.Fatal("a deadline on 31 February was accepted")
	}
	stranger := seedApprovalUser(t, d, "deadline-stranger@test.local")
	if _, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Чужой лид", Slug: "", Color: "#7c3aed", LeadUserID: stranger}); err == nil {
		t.Fatal("a lead outside the workspace was accepted")
	}
}
