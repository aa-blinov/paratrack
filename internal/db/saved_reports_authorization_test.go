package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestDeleteSavedReportForActorEnforcesCurrentRoleAndOwnership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Saved reports", "saved-reports")
	ownerID := teamOwner(t, d, teamID)
	adminID := seedProjectTestUser(t, d, "saved-report-admin")
	memberID := seedProjectTestUser(t, d, "saved-report-member")
	outsiderID := seedProjectTestUser(t, d, "saved-report-outsider")
	joinedAt := FormatTime(time.Now().UTC())
	for _, membership := range []struct {
		userID int64
		role   string
	}{{adminID, "admin"}, {memberID, "member"}} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)`,
			teamID, membership.userID, membership.role, joinedAt); err != nil {
			t.Fatal(err)
		}
	}

	ownerReport, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{TeamID: teamID, ActorID: ownerID, Name: "owner report", Period: "week"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{TeamID: teamID, ActorID: outsiderID, Name: "outsider report", Period: "week"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("outsider creating report = %v, want forbidden", err)
	}
	if err := d.DeleteSavedReportForActor(ctx, appmodel.SavedReportDeleteRequest{TeamID: teamID, ReportID: ownerReport.ID, CallerID: memberID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member deleting another user's report = %v, want not found", err)
	}
	if err := d.DeleteSavedReportForActor(ctx, appmodel.SavedReportDeleteRequest{TeamID: teamID, ReportID: ownerReport.ID, CallerID: adminID}); err != nil {
		t.Fatalf("admin deleting another user's report: %v", err)
	}

	memberReport, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{TeamID: teamID, ActorID: memberID, Name: "member report", Period: "week"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSavedReportForActor(ctx, appmodel.SavedReportDeleteRequest{TeamID: teamID, ReportID: memberReport.ID, CallerID: memberID}); err != nil {
		t.Fatalf("member deleting own report: %v", err)
	}

	removedMemberReport, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{TeamID: teamID, ActorID: memberID, Name: "removed member report", Period: "week"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, memberID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSavedReportForActor(ctx, appmodel.SavedReportDeleteRequest{TeamID: teamID, ReportID: removedMemberReport.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member deleting report = %v, want forbidden", err)
	}
}
