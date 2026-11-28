package db

import (
	"errors"
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
	// The preview says what would change and writes nothing.
	preview, err := d.PreviewPayrollRecalculation(ownerCtx, appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run2.ID, CallerID: ownerID, At: now})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.BeforeCents != 262000 || preview.AfterCents != 368000 || len(preview.People) != 1 || preview.People[0].AfterCents != 368000 {
		t.Fatalf("preview: %+v", preview)
	}
	if len(preview.NotInRun) != 1 {
		t.Fatalf("dev1 is paid by the period but not on this run: %+v", preview.NotInRun)
	}
	unchanged, err := d.GetPayrollRunDetails(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: run2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Lines[0].AmountCents != 200000 {
		t.Fatalf("the preview wrote: %+v", unchanged.Lines)
	}
	// Without a confirmed preview, or with one whose numbers moved, nothing
	// is applied.
	for _, expect := range []string{"", preview.Fingerprint + "x"} {
		if _, err := d.RecalculatePayrollDraft(ownerCtx, appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run2.ID, CallerID: ownerID, At: now, Expect: expect}); !errors.Is(err, model.ErrPayrollChanged) {
			t.Fatalf("confirmation %q: err = %v", expect, err)
		}
	}
	if _, err := d.RecalculatePayrollDraft(ownerCtx, appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run2.ID, CallerID: ownerID, At: now, Expect: preview.Fingerprint}); err != nil {
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
	if after.Lines[0].Kind != "" {
		t.Fatalf("the priced line must come before the adjustments after a recalculation: %+v", after.Lines)
	}

	// All of dev2's pay goes away (rate zeroed): the percent comes to zero and
	// the result names them. When the rate comes back, so does dev2.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 0 WHERE team_id = ? AND user_id = ?`, teamID, dev2ID); err != nil {
		t.Fatal(err)
	}
	// apply confirms a fresh preview, the way the screen does.
	apply := func() (appmodel.PayrollRecalculateResult, error) {
		req := appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run2.ID, CallerID: ownerID, At: now}
		p, err := d.PreviewPayrollRecalculation(ownerCtx, req)
		if err != nil {
			return p, err
		}
		req.Expect = p.Fingerprint
		return d.RecalculatePayrollDraft(ownerCtx, req)
	}
	res, err := apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NoEarnings) != 1 || res.BeforeCents != 368000 || res.AfterCents != 50000 {
		t.Fatalf("zeroed recalculation result: %+v", res)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 300000 WHERE team_id = ? AND user_id = ?`, teamID, dev2ID); err != nil {
		t.Fatal(err)
	}
	if res, err = apply(); err != nil || res.AfterCents != 368000 || len(res.NoEarnings) != 0 {
		t.Fatalf("dev2 did not come back: %+v err=%v", res, err)
	}
	after, err = d.GetPayrollRunDetails(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: run2.ID})
	if err != nil {
		t.Fatal(err)
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

// Hours moved from one person to another with the total unchanged: the
// preview must say something changed, and a confirmation made before the
// move must not apply after it.
func TestPayrollRecalculationSeesMoneyMovingBetweenPeople(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	ownerID := seedApprovalUser(t, d, "owner-pay-shift@test.local")
	annaID := seedApprovalUser(t, d, "anna-pay-shift@test.local")
	borisID := seedApprovalUser(t, d, "boris-pay-shift@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "pay-shift", Name: "Shift", OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{annaID, borisID} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, 200000)`,
			teamID, uid, FormatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	var actID int64
	if err := d.TestSQL().QueryRowContext(ctx, `INSERT INTO activities (name, team_id) VALUES ('Dev', ?) RETURNING id`, teamID).Scan(&actID); err != nil {
		t.Fatal(err)
	}
	weekStart := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, annaID, actID, weekStart.Add(9*time.Hour))
	seedClosedSession(t, d, teamID, borisID, actID, weekStart.Add(11*time.Hour))
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekStart.AddDate(0, 0, 7)}); err != nil {
		t.Fatal(err)
	}
	run, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{TeamID: teamID, CallerID: ownerID, CreatedAt: now, Start: weekStart, End: weekStart.AddDate(0, 0, 7)})
	if err != nil {
		t.Fatal(err)
	}
	req := appmodel.PayrollRecalculateRequest{TeamID: teamID, RunID: run.ID, CallerID: ownerID, At: now}
	before, err := d.PreviewPayrollRecalculation(ownerCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	if before.Changed {
		t.Fatalf("nothing moved yet: %+v", before)
	}
	// Anna's rate goes down by what Boris's goes up: the total is the same.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 150000 WHERE team_id = ? AND user_id = ?`, teamID, annaID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 250000 WHERE team_id = ? AND user_id = ?`, teamID, borisID); err != nil {
		t.Fatal(err)
	}
	moved, err := d.PreviewPayrollRecalculation(ownerCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Changed || moved.BeforeCents != moved.AfterCents {
		t.Fatalf("money moved between two people with the total unchanged: %+v", moved)
	}
	req.Expect = before.Fingerprint
	if _, err := d.RecalculatePayrollDraft(ownerCtx, req); !errors.Is(err, model.ErrPayrollChanged) {
		t.Fatalf("a confirmation made before the move applied after it: err = %v", err)
	}
	req.Expect = moved.Fingerprint
	if _, err := d.RecalculatePayrollDraft(ownerCtx, req); err != nil {
		t.Fatal(err)
	}
}
