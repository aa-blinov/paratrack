package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A person sees their own pay lines in their workspace, newest period first, and
// nobody else's (FIN-11). Another workspace's runs do not show up.
func TestMemberSeesOnlyTheirOwnPayLines(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Начисления", "member-pay-lines")
	otherTeamID := seedTeam(t, d, "Чужая", "member-pay-other")
	ownerID := teamOwner(t, d, teamID)
	viewerID := seedProjectTestUser(t, d, "pay-viewer")
	colleagueID := seedProjectTestUser(t, d, "pay-colleague")
	addMember(t, d, teamID, viewerID, "member")
	addMember(t, d, teamID, colleagueID, "member")

	insertRun := func(team int64, number string, start time.Time, status string) int64 {
		t.Helper()
		var id int64
		if err := d.TestSQL().QueryRowContext(ctx,
			`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, created_at)
			 VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			team, number, FormatTime(start), FormatTime(start.AddDate(0, 0, 7)), status, FormatTime(start)).Scan(&id); err != nil {
			t.Fatalf("seed run %s: %v", number, err)
		}
		return id
	}
	insertLine := func(run, user int64, seconds, rate, amount int) {
		t.Helper()
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, 'Работа', ?, ?, ?)`,
			run, user, seconds, rate, amount); err != nil {
			t.Fatalf("seed line: %v", err)
		}
	}

	march := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	october := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	paidRun := insertRun(teamID, "PAY-2026-001", march, "paid")
	openRun := insertRun(teamID, "PAY-2026-002", october, "draft")
	insertLine(paidRun, viewerID, 7200, 3000, 6000)
	insertLine(openRun, viewerID, 3600, 3000, 3000)
	insertLine(openRun, colleagueID, 9000, 3000, 7500)
	insertLine(insertRun(otherTeamID, "PAY-2026-001", october, "draft"), viewerID, 3600, 3000, 3000)

	lines, err := d.ListMemberPayrollLines(ctx, appmodel.MemberPayrollQuery{TeamID: teamID, UserID: viewerID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("the viewer sees %d lines, want their two in this workspace", len(lines))
	}
	if lines[0].RunNumber != "PAY-2026-002" || lines[0].RunStatus != "draft" || lines[0].AmountCents != 3000 {
		t.Fatalf("the newest line is %+v, want the October draft", lines[0])
	}
	if lines[1].RunNumber != "PAY-2026-001" || lines[1].RunStatus != "paid" || lines[1].AmountCents != 6000 || lines[1].Currency == "" {
		t.Fatalf("the older line is %+v, want the paid March run", lines[1])
	}
	for _, line := range lines {
		if line.RateCents != 3000 {
			t.Fatalf("a line lost its rate: %+v", line)
		}
	}

	none, err := d.ListMemberPayrollLines(ctx, appmodel.MemberPayrollQuery{TeamID: teamID, UserID: ownerID})
	if err != nil || len(none) != 0 {
		t.Fatalf("the owner, with no lines, sees %+v (err %v)", none, err)
	}
}
