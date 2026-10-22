package web

import (
	"context"
	"testing"
)

// The profile lists the viewer's own pay in this workspace, with what is still to be
// paid and what is paid out (FIN-11). It is read-only: the page offers no pay form.
func TestProfileShowsTheViewersOwnPay(t *testing.T) {
	e := newAPIEnv(t)
	e.register("profile-pay@x.test")
	ctx := context.Background()
	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT team_id, user_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatalf("find the owner: %v", err)
	}
	var runID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, created_at)
		 VALUES (?, 'PAY-2026-010', '2026-10-01T00:00:00Z', '2026-10-08T00:00:00Z', 'paid', '2026-10-08T09:00:00Z') RETURNING id`,
		teamID).Scan(&runID); err != nil {
		t.Fatalf("seed the pay run: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx,
		`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, 'Работа', 3600, 3000, 3000)`,
		runID, ownerID); err != nil {
		t.Fatalf("seed the pay line: %v", err)
	}

	page := reactData[profilePageData](t, readBody(t, e.do("GET", "/settings/profile", nil, nil)))
	if len(page.Pay) != 1 {
		t.Fatalf("the profile shows %d pay lines, want the one the owner has", len(page.Pay))
	}
	if row := page.Pay[0]; row.Run != "PAY-2026-010" || !row.Paid || row.Amount == "" {
		t.Fatalf("the pay line is %+v, want the paid run with an amount", row)
	}
	if page.PayPaid == "" {
		t.Fatal("the paid total is empty for a paid line")
	}
}
