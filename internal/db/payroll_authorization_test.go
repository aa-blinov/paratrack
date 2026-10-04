package db

import (
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestPayrollWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	run, err := d.createPayrollRun(ctx, preparedPayrollRunRequest{TeamID: teamID, CallerID: ownerID, Number: "PAY-2026-001", Start: start, End: start.AddDate(0, 0, 7)})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if _, err := d.createPayrollRun(ctx, preparedPayrollRunRequest{TeamID: teamID, CallerID: memberID, Number: "PAY-2026-002", Start: start, End: start.AddDate(0, 0, 7)}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("createPayrollRun error = %v, want forbidden", err)
	}
	if _, _, err := d.CreatePayrollDraft(ctx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: memberID, CreatedAt: time.Now().UTC(), Start: start,
		End: start.AddDate(0, 0, 7), ConfirmOverlap: true,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("CreatePayrollDraft error = %v, want forbidden", err)
	}
	if _, _, err := d.MarkPayrollPaidWithRecipients(ctx, appmodel.PayrollMutationRequest{TeamID: teamID, RunID: run.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("MarkPayrollPaidWithRecipients error = %v, want forbidden", err)
	}
	if err := d.DeletePayrollDraft(ctx, appmodel.PayrollMutationRequest{TeamID: teamID, RunID: run.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("DeletePayrollDraft error = %v, want forbidden", err)
	}

	got, err := d.GetPayrollRun(ctx, teamID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "draft" {
		t.Errorf("unauthorized writes changed payroll status to %q", got.Status)
	}
}
