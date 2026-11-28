package db

import (
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestPayrollUserFilterAndPercentAdjustment(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

	ownerID := seedApprovalUser(t, d, "owner-pay-filter@test.local")
	dev1ID := seedApprovalUser(t, d, "dev1-pay-filter@test.local")
	dev2ID := seedApprovalUser(t, d, "dev2-pay-filter@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "pay-filter", Name: "PayFilter", OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{dev1ID, dev2ID} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, 200000)`,
			teamID, uid, FormatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	var actID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Dev', ?) RETURNING id`, teamID).Scan(&actID); err != nil {
		t.Fatal(err)
	}

	weekStart := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	seedClosedSession(t, d, teamID, dev1ID, actID, weekStart.Add(9*time.Hour))
	seedClosedSession(t, d, teamID, dev2ID, actID, weekStart.Add(11*time.Hour))

	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd,
	}); err != nil {
		t.Fatal(err)
	}

	// Run 1: dev1 only.
	run1, overlaps, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, CreatedAt: now, Start: weekStart, End: weekEnd,
		UserIDs: []int64{dev1ID},
	})
	if err != nil || len(overlaps) > 0 {
		t.Fatalf("run1: err=%v overlaps=%v", err, overlaps)
	}

	// Run 2: dev2 only over the exact same period -> no overlap because members do not intersect.
	run2, overlaps, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, CreatedAt: now, Start: weekStart, End: weekEnd,
		UserIDs: []int64{dev2ID},
	})
	if err != nil || len(overlaps) > 0 {
		t.Fatalf("run2 for disjoint member should not overlap: err=%v overlaps=%v", err, overlaps)
	}

	// Run 3: dev1 again -> must report overlap with run1.
	_, overlaps, err = d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, CreatedAt: now, Start: weekStart, End: weekEnd,
		UserIDs: []int64{dev1ID},
	})
	if err != nil || len(overlaps) != 1 || overlaps[0].ID != run1.ID {
		t.Fatalf("expected overlap with run1, got err=%v overlaps=%+v", err, overlaps)
	}

	// A fixed bonus first: the percent below must not be taken of it.
	if err := d.AddPayrollAdjustment(ownerCtx, appmodel.PayrollAdjustmentRequest{
		TeamID: teamID, RunID: run2.ID, CallerID: ownerID, UserID: dev2ID,
		Label: "Премия", AmountCents: 50000,
	}); err != nil {
		t.Fatalf("fixed adjustment: %v", err)
	}
	// Percent adjustment (+6% on 1h * 2000 RUB = 2000 RUB -> +120 RUB = 12000 cents).
	if err := d.AddPayrollAdjustment(ownerCtx, appmodel.PayrollAdjustmentRequest{
		TeamID: teamID, RunID: run2.ID, CallerID: ownerID, UserID: dev2ID,
		Label: "Налог самозанятого", PercentBps: 600,
	}); err != nil {
		t.Fatalf("percent adjustment: %v", err)
	}
	details, err := d.GetPayrollRunDetails(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: run2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(details.Lines))
	}
	if details.Lines[2].AmountCents != 12000 {
		t.Fatalf("expected 12000 cents (+6%% of the hours, not of the bonus), got %d", details.Lines[2].AmountCents)
	}
	if details.Lines[0].Kind != "" || details.Lines[1].Kind != model.PayrollLineAdjustment || details.Lines[2].Kind != model.PayrollLineAdjustment {
		t.Fatalf("unexpected kinds: %+v", details.Lines)
	}
	// The rate changes after the draft was made. Recalculating prices the hour
	// at the new rate, keeps the fixed bonus and takes the 6% of the new sum.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 300000 WHERE team_id = ? AND user_id = ?`, teamID, dev2ID); err != nil {
		t.Fatal(err)
	}
	if err := d.RecalculatePayrollDraft(ownerCtx, appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run2.ID, CallerID: ownerID, At: now}); err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	after, err := d.GetPayrollRunDetails(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: run2.ID})
	if err != nil {
		t.Fatal(err)
	}
	amounts := map[string]int{}
	for _, l := range after.Lines {
		switch {
		case l.Kind == "":
			amounts["hours"] += l.AmountCents
		case strings.Contains(l.Label, "%"):
			amounts["percent"] += l.AmountCents
		default:
			amounts["fixed"] += l.AmountCents
		}
	}
	if amounts["hours"] != 300000 || amounts["fixed"] != 50000 || amounts["percent"] != 18000 || len(after.Lines) != 3 {
		t.Fatalf("after recalculation: %+v (lines %+v)", amounts, after.Lines)
	}
	var pricedID int64
	for _, l := range after.Lines {
		if l.Kind == "" {
			pricedID = l.ID
		}
	}
	// Only an adjustment can be deleted; the priced line stays.
	if err := d.DeletePayrollAdjustment(ownerCtx, appmodel.PayrollAdjustmentDeleteRequest{
		TeamID: teamID, RunID: run2.ID, CallerID: ownerID, LineID: pricedID,
	}); err == nil {
		t.Fatal("a priced line was deleted as an adjustment")
	}
}
