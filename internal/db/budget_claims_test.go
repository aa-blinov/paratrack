package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
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

// Adding or removing a draft's line is a new revision of it, so a look kept
// for a letter file before the change is kept again at send.
func TestDraftLineEditsBumpTheRevision(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Ревизия", "line-revision")
	ownerID := teamOwner(t, d, teamID)
	actor := requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(actor, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-R", Client: "К", Start: start, End: start.AddDate(0, 1, 0),
		Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 3600, RateCents: 100000, AmountCents: 100000}}})
	if err != nil {
		t.Fatal(err)
	}
	revision := func() int64 {
		var r int64
		d.TestSQL().QueryRowContext(ctx, `SELECT revision FROM invoices WHERE id = ?`, inv.ID).Scan(&r)
		return r
	}
	before := revision()
	if err := d.AddInvoiceLine(actor, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Label: "Доплата", AmountCents: 500}); err != nil {
		t.Fatal(err)
	}
	afterAdd := revision()
	var lineID int64
	d.TestSQL().QueryRowContext(ctx, `SELECT id FROM invoice_lines WHERE invoice_id = ? AND label = 'Доплата'`, inv.ID).Scan(&lineID)
	if err := d.DeleteInvoiceLine(actor, appmodel.InvoiceLineDeleteRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, LineID: lineID}); err != nil {
		t.Fatal(err)
	}
	if afterAdd <= before || revision() <= afterAdd {
		t.Fatalf("revisions %d → %d → %d, want each edit to raise it", before, afterAdd, revision())
	}
}

// An invoice over a project without a rate bills each member at their rate
// for the client, a line per person.
func TestInvoiceBillsMembersAtTheirRateWithoutAProjectRate(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Роли-счёт", "invoice-roles")
	ownerID := teamOwner(t, d, teamID)
	now := FormatTime(time.Now().UTC())
	var projectID, activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, client_name, created_at, updated_at) VALUES (?, 'ir', 'IR', '#000000', '', ?, ?) RETURNING id`,
		teamID, now, now).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (team_id, name, name_key, project_id, created_at) VALUES (?, 'Дизайн', 'дизайн-ir', ?, ?) RETURNING id`,
		teamID, projectID, now).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET bill_rate_cents = 300000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, activityID, day)
	lines, err := d.BuildInvoiceLines(ctx, teamID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].RateCents != 300000 || lines[0].AmountCents != 300000 || len(lines[0].Label) <= len("IR · Дизайн") {
		t.Fatalf("lines at a member's rate: %+v", lines)
	}
}
