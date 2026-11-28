package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A budget warning never steps back for the same figures: after "will go
// over" (90), "nearing the limit" (80) is not sent; new figures start over.
func TestBudgetWarningsNeverStepBack(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Бюджет", "budget-claims")
	var projectID int64
	now := FormatTime(time.Now().UTC())
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO projects (team_id, slug, name, color, client_name, created_at, updated_at) VALUES (?, 'b', 'B', '#000000', '', ?, ?) RETURNING id`,
		teamID, now, now).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	claim := func(level int, basis string) bool {
		ok, err := d.ClaimBudgetNotice(t.Context(), appmodel.BudgetNoticeRequest{ProjectID: projectID, Level: level, Basis: basis})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !claim(90, "600/0/0") {
		t.Fatal("the forecast warning was not claimed")
	}
	if claim(80, "600/0/0") {
		t.Fatal("nearing the limit came after will-go-over for the same figures")
	}
	if !claim(100, "600/0/0") {
		t.Fatal("went over was refused after will-go-over")
	}
	if !claim(80, "1200/0/0") {
		t.Fatal("new figures did not start the warnings over")
	}
}

// A project without a rate of its own is priced at each member's rate for
// the client; one with a rate keeps pricing at its own.
func TestBudgetPricesAtMemberRateWithoutAProjectRate(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Роли", "budget-roles")
	ownerID := teamOwner(t, d, teamID)
	now := FormatTime(time.Now().UTC())
	var projectID, activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, client_name, budget_cents, created_at, updated_at) VALUES (?, 'r', 'R', '#000000', '', 1000000, ?, ?) RETURNING id`,
		teamID, now, now).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (team_id, name, name_key, project_id, created_at) VALUES (?, 'Дизайн', 'дизайн', ?, ?) RETURNING id`,
		teamID, projectID, now).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET bill_rate_cents = 200000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	seedClosedSession(t, d, teamID, ownerID, activityID, time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC))
	budgets, err := d.ProjectBudgetSummaries(ctx, appmodel.ProjectBudgetQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if got := budgets[projectID].SpentCents; got != 200000 {
		t.Fatalf("an hour at a member rate of 2000/h spent %d", got)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE projects SET billable_rate_cents = 100000 WHERE id = ?`, projectID); err != nil {
		t.Fatal(err)
	}
	budgets, err = d.ProjectBudgetSummaries(ctx, appmodel.ProjectBudgetQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if got := budgets[projectID].SpentCents; got != 100000 {
		t.Fatalf("the project's own rate of 1000/h did not win: %d", got)
	}
}
