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
