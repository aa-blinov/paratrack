package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The recalculation as the manager meets it, through the handlers: a person
// with only a bonus stands on their own row, a change of hours alone shows in
// the preview and lands in the audit log with the person's id, and a pay run
// that is no longer a draft answers in words about recalculating.
func TestPayrollRecalculationThroughTheScreens(t *testing.T) {
	e := newAPIEnv(t)
	e.register("pay-screens@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"studio"}, "from": {"welcome"}}, nil).Body.Close()
	ctx := t.Context()
	sql := e.db.TestSQL()
	var teamID, ownerID, yanaID int64
	if err := sql.QueryRowContext(ctx, `SELECT m.team_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'pay-screens@x.test'`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	if err := sql.QueryRowContext(ctx, `INSERT INTO users (email, password_hash, name) VALUES ('yana@x.test', 'x', 'Яна') RETURNING id`).Scan(&yanaID); err != nil {
		t.Fatal(err)
	}
	if _, err := sql.ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', '2026-01-01T00:00:00Z', 100000)`, teamID, yanaID); err != nil {
		t.Fatal(err)
	}
	if _, err := sql.ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 200000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}

	fortnight := time.Now().AddDate(0, 0, -14)
	monday := fortnight.AddDate(0, 0, -(int(fortnight.Weekday())+6)%7)
	day := fortnight.Format("2006-01-02")
	e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Вёрстка"}, "start": {day + " 09:00"}, "end": {day + " 11:00"}}, map[string]string{"HX-Request": "true"}).Body.Close()
	week := url.Values{"start": {monday.Format("2006-01-02")}, "end": {monday.AddDate(0, 0, 6).Format("2006-01-02")}}
	e.do("POST", "/api/timesheet/approve", week, nil).Body.Close()

	create := e.do("POST", "/payroll", url.Values{"start": week["start"], "end": {monday.AddDate(0, 0, 7).Format("2006-01-02")},
		"user_id": {strconv.FormatInt(ownerID, 10), strconv.FormatInt(yanaID, 10)}}, nil)
	create.Body.Close()
	runPath := strings.SplitN(create.Header.Get("Location"), "?", 2)[0]
	if !strings.HasPrefix(runPath, "/payroll/") {
		t.Fatalf("create the run: %d %q", create.StatusCode, create.Header.Get("Location"))
	}
	for _, adj := range []struct {
		user  int64
		label string
	}{{yanaID, "Премия"}, {ownerID, "Компенсация"}} {
		e.do("POST", runPath+"/adjustments", url.Values{"user_id": {strconv.FormatInt(adj.user, 10)}, "label": {adj.label}, "amount": {"300"}}, nil).Body.Close()
	}

	detail := reactData[payrollDetailPage](t, readBody(t, e.do("GET", runPath, nil, nil)))
	under := map[string]bool{}
	for _, l := range detail.Run.Lines {
		under[l.Label] = l.Under
	}
	if len(detail.Run.Lines) != 3 || !under[detail.Run.Lines[1].Label] || under["Яна — Премия"] {
		t.Fatalf("the owner's compensation sits under their line, Yana's bonus on its own row: %+v", detail.Run.Lines)
	}

	// Thirty-six seconds more: the money may round the same, the hours do not.
	if _, err := sql.ExecContext(ctx, `UPDATE sessions SET accumulated_seconds = accumulated_seconds + 36 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	preview := reactData[payrollDetailPage](t, readBody(t, e.do("GET", runPath+"?preview=recalculate", nil, nil)))
	if preview.RecalcPreview == nil || !preview.RecalcPreview.Changed || preview.RecalcPreview.Expect == "" {
		t.Fatalf("preview: %+v", preview.RecalcPreview)
	}
	var owner *payrollRecalcPersonVM
	for i := range preview.RecalcPreview.People {
		if preview.RecalcPreview.People[i].UserID == ownerID {
			owner = &preview.RecalcPreview.People[i]
		}
	}
	if owner == nil || !owner.HoursChanged || owner.HoursBefore == owner.HoursAfter {
		t.Fatalf("the owner's hours change is not shown: %+v", owner)
	}

	apply := e.do("POST", runPath+"/recalculate", url.Values{"expect": {preview.RecalcPreview.Expect}}, nil)
	apply.Body.Close()
	if loc := apply.Header.Get("Location"); strings.Contains(loc, "preview=") {
		t.Fatalf("the confirmed preview did not apply: %q", loc)
	}
	var meta string
	if err := sql.QueryRowContext(ctx, `SELECT meta FROM audit_log WHERE team_id = ? AND action = 'payroll.recalculate' ORDER BY id DESC LIMIT 1`, teamID).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta, fmt.Sprintf("(#%d)", ownerID)) || !strings.Contains(meta, "s -> ") {
		t.Fatalf("audit meta lacks the owner's id and hours: %q", meta)
	}

	e.do("POST", runPath+"/paid", nil, nil).Body.Close()
	paid := e.do("POST", runPath+"/recalculate", nil, nil)
	paid.Body.Close()
	after := reactData[payrollDetailPage](t, readBody(t, e.do("GET", paid.Header.Get("Location"), nil, nil)))
	if !strings.Contains(after.Flash, "Пересчитать можно только черновик") {
		t.Fatalf("a paid run answers about recalculating: %q", after.Flash)
	}
}
