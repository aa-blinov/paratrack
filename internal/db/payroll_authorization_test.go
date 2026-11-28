package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// seedDraftPayrollRun gives a workspace one draft run for the write guards to
// aim at, without pricing anything.
func seedDraftPayrollRun(t *testing.T, d *DB, teamID int64, number string, start time.Time) int64 {
	t.Helper()
	var runID int64
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, created_at)
		 VALUES (?, ?, ?, ?, 'draft', '', ?) RETURNING id`,
		teamID, number, FormatTime(start), FormatTime(start.AddDate(0, 0, 7)), FormatTime(time.Now().UTC())).Scan(&runID); err != nil {
		t.Fatalf("seed payroll run: %v", err)
	}
	return runID
}

func TestPayrollWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	runID := seedDraftPayrollRun(t, d, teamID, "PAY-2026-001", start)
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if _, _, err := d.CreatePayrollDraft(ctx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: memberID, CreatedAt: time.Now().UTC(), Start: start,
		End: start.AddDate(0, 0, 7), ConfirmOverlap: true,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("CreatePayrollDraft error = %v, want forbidden", err)
	}
	if _, _, err := d.MarkPayrollPaidWithRecipients(ctx, appmodel.PayrollMutationRequest{TeamID: teamID, RunID: runID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("MarkPayrollPaidWithRecipients error = %v, want forbidden", err)
	}
	if err := d.DeletePayrollDraft(ctx, appmodel.PayrollMutationRequest{TeamID: teamID, RunID: runID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("DeletePayrollDraft error = %v, want forbidden", err)
	}

	got, err := d.GetPayrollRun(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "draft" {
		t.Errorf("unauthorized writes changed payroll status to %q", got.Status)
	}
}
