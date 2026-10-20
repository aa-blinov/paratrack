package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// An empty pay run says why: nobody has an hourly rate, or nobody tracked
// time in the period. The two need different fixes.
func TestEmptyPayRunSaysWhetherRatesOrTimeAreMissing(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "rates-owner@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "rates-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	draft := func() error {
		_, _, err := d.CreatePayrollDraft(ctx, appmodel.PayrollDraftRequest{TeamID: teamID, CallerID: ownerID, Start: start, End: end, CreatedAt: time.Now().UTC()})
		return err
	}
	if err := draft(); !errors.Is(err, model.ErrNoPayRates) {
		t.Fatalf("nobody has a rate: %v, want ErrNoPayRates", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 4000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatalf("set rate: %v", err)
	}
	if err := draft(); !errors.Is(err, model.ErrNoPayableTime) {
		t.Fatalf("rated but no time: %v, want ErrNoPayableTime", err)
	}
}
