package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A draft changes the client's card only when the manager asks for it, and
// an empty field never clears what the card already holds: the card serves
// every project of that client.
func TestDraftEditUpdatesClientCardOnlyWhenAsked(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Карточка", "client-card")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	var clientID, projectID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name, email, details) VALUES (?, 'Ромашка', 'old@romashka.test', 'ИНН 7707083893, договор №12') RETURNING id`, teamID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name, slug, client_id) VALUES (?, 'Сайт', 'card-site', ?) RETURNING id`, teamID, clientID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-970", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	card := func() (name, email, details string) {
		t.Helper()
		if err := d.TestSQL().QueryRowContext(ctx, `SELECT name, email, details FROM clients WHERE id = ?`, clientID).Scan(&name, &email, &details); err != nil {
			t.Fatal(err)
		}
		return
	}
	edit := func(email, details string, updateCard bool) {
		t.Helper()
		if err := d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
			TeamID: teamID, InvoiceID: inv.ID, ProjectID: projectID, CallerID: ownerID,
			Client: "Ромашка", Email: email, Details: details, UpdateClientCard: updateCard,
		}); err != nil {
			t.Fatalf("edit: %v", err)
		}
	}

	// Details that do not check out (an old draft) never reach the project's
	// copy, which the next invoice falls back to.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE projects SET client_details = 'ИНН 7707083893' WHERE id = ?`, projectID); err != nil {
		t.Fatal(err)
	}
	edit("old@romashka.test", "ИНН 7707083894", false)
	var projectCopy string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT client_details FROM projects WHERE id = ?`, projectID).Scan(&projectCopy); err != nil {
		t.Fatal(err)
	}
	if projectCopy != "ИНН 7707083893" {
		t.Fatalf("a wrong number reached the project's copy: %q", projectCopy)
	}
	edit("new@romashka.test", "ИНН 500100732259", false)
	if _, email, details := card(); email != "old@romashka.test" || details != "ИНН 7707083893, договор №12" {
		t.Fatalf("the card changed without being asked: %q %q", email, details)
	}
	edit("new@romashka.test", "", true)
	if _, email, details := card(); email != "new@romashka.test" || details != "ИНН 7707083893, договор №12" {
		t.Fatalf("asked, with empty details: %q %q", email, details)
	}
	// An invoice over every project has no card to update: refused, not skipped.
	if err := d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
		TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Client: "Ромашка", UpdateClientCard: true,
	}); !errors.Is(err, model.ErrNoClientCard) {
		t.Fatalf("card update on an invoice with no project: err = %v", err)
	}
	edit("", "ИНН 500100732259", true)
	if _, email, details := card(); email != "new@romashka.test" || details != "ИНН 500100732259" {
		t.Fatalf("asked, with new details and no email: %q %q", email, details)
	}
}

// Projects that kept a client only as their own copy get the client's card on
// the next start, so the card is the one place a client lives.
func TestProjectClientCopiesBecomeCards(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Копии", "client-copies")
	var projectID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name, slug, client_name, client_email, client_details) VALUES (?, 'Лендинг', 'copies-landing', 'Лютик', 'buh@lutik.test', 'ИНН 500100732259') RETURNING id`,
		teamID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = '20261128_project_client_copies_to_cards'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateProjectCopiesToCards(ctx); err != nil {
		t.Fatal(err)
	}
	var name, email, details string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT c.name, c.email, c.details FROM projects p JOIN clients c ON c.id = p.client_id WHERE p.id = ?`, projectID).Scan(&name, &email, &details); err != nil {
		t.Fatalf("the project has no card: %v", err)
	}
	if name != "Лютик" || email != "buh@lutik.test" || details != "ИНН 500100732259" {
		t.Fatalf("card %q %q %q", name, email, details)
	}
}

// Nobody away, and nobody at all on a workspace holiday, is asked to fill
// the day; old absence day marks move to their own table.
func TestDayFillSkipsAbsencesAndHolidays(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Отпуска", "day-fill-away")
	ownerID := teamOwner(t, d, teamID)
	awayID := seedApprovalUser(t, d, "away-fill@test.local")
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', '2026-01-01T00:00:00Z')`, teamID, awayID); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	query := appmodel.DayFillCandidatesQuery{TeamID: teamID, From: day, To: day.AddDate(0, 0, 1), Day: "2026-11-25"}
	if err := d.SetAbsence(ctx, appmodel.AbsenceRequest{TeamID: teamID, UserID: awayID, Days: []string{"2026-11-25"}, Kind: "vacation", Now: day}); err != nil {
		t.Fatal(err)
	}
	ids, err := d.DayFillCandidates(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != ownerID {
		t.Fatalf("asked to fill the day: %v, want only the owner", ids)
	}
	if err := d.SetTeamHolidays(ctx, appmodel.TeamHolidayRequest{TeamID: teamID, Days: []string{"2026-11-25"}, Name: "Праздник"}); err != nil {
		t.Fatal(err)
	}
	if ids, err = d.DayFillCandidates(ctx, query); err != nil || len(ids) != 0 {
		t.Fatalf("asked on a holiday: %v %v", ids, err)
	}

	// An absence written as a day mark before is moved, once.
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO day_marks (team_id, user_id, day, kind, note, created_at) VALUES (?, ?, '2026-11-26', 'sick', '', '2026-11-26T00:00:00Z')`, teamID, awayID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = '20261128_absences_out_of_day_marks'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateAbsencesOutOfDayMarks(ctx); err != nil {
		t.Fatal(err)
	}
	var marks, absences int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM day_marks WHERE team_id = ?), (SELECT COUNT(*) FROM member_absences WHERE team_id = ? AND day = '2026-11-26')`, teamID, teamID).Scan(&marks, &absences); err != nil {
		t.Fatal(err)
	}
	if marks != 0 || absences != 1 {
		t.Fatalf("after the move: %d marks, %d absences", marks, absences)
	}
}

// Running timers come with their owner's threshold: 4 hours by default, the
// person's own when set, and a reminded session is not listed again.
func TestRunningSessionsCarryTheThreshold(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Таймеры", "long-timers")
	ownerID := teamOwner(t, d, teamID)
	setID := seedApprovalUser(t, d, "long-set@test.local")
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE users SET prefs = '{"long_timer_hours":2,"duration":"hm"}' WHERE id = ?`, setID); err != nil {
		t.Fatal(err)
	}
	var actID int64
	if err := d.TestSQL().QueryRowContext(ctx, `INSERT INTO activities (name, team_id) VALUES ('Код', ?) RETURNING id`, teamID).Scan(&actID); err != nil {
		t.Fatal(err)
	}
	ids := map[int64]int64{}
	for _, uid := range []int64{ownerID, setID} {
		var id int64
		if err := d.TestSQL().QueryRowContext(ctx,
			`INSERT INTO sessions (activity_id, team_id, user_id, start_at, last_resume_at) VALUES (?, ?, ?, '2026-11-11T08:00:00Z', '2026-11-11T08:00:00Z') RETURNING id`,
			actID, teamID, uid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[uid] = id
	}
	running, err := d.RunningSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	threshold := map[int64]int{}
	for _, r := range running {
		if r.TeamID == teamID {
			threshold[r.UserID] = r.ThresholdHours
		}
	}
	if threshold[ownerID] != 4 || threshold[setID] != 2 {
		t.Fatalf("thresholds: %v", threshold)
	}
	if ok, err := d.ClaimLongTimerNotice(ctx, appmodel.LongTimerNoticeRequest{SessionID: ids[setID]}); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if running, err = d.RunningSessions(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range running {
		if r.SessionID == ids[setID] {
			t.Fatal("a reminded session is listed again")
		}
	}
}

// Projects with an estimate or a budget come with the time tracked on them.
func TestBudgetProjectsSumTrackedTime(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Бюджеты", "budget-projects")
	ownerID := teamOwner(t, d, teamID)
	var projectID, actID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name, slug, estimate_minutes, budget_cents, billable_rate_cents) VALUES (?, 'Сайт', 'budget-site', 600, 1000000, 200000) RETURNING id`, teamID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx, `INSERT INTO activities (name, team_id, project_id) VALUES ('Вёрстка', ?, ?) RETURNING id`, teamID, projectID).Scan(&actID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds) VALUES (?, ?, ?, '2026-11-02T09:00:00Z', '2026-11-02T12:00:00Z', 10800)`,
		actID, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	projects, err := d.BudgetProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.ProjectID == projectID {
			if p.TrackedSeconds != 10800 || p.EstimateMinutes != 600 || p.BudgetCents != 1000000 || p.RateCents != 200000 {
				t.Fatalf("budget project: %+v", p)
			}
			return
		}
	}
	t.Fatal("the project is not listed")
}
