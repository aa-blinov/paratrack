package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The invoice's sessions appendix downloads as CSV, one row per billed session, with
// the day and the times in the viewer's zone (FIN-06).
func TestInvoiceSessionsAppendixDownloadsAsCSV(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-appendix@x.test")
	ctx := context.Background()
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT team_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("find the workspace: %v", err)
	}
	saved := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Appendix work"}, "start": {"20.10.2026 09:00"}, "end": {"20.10.2026 10:30"},
	}, nil)
	readBody(t, saved)
	if saved.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("the session was not saved: status %d", saved.StatusCode)
	}
	var sessionID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT s.id FROM sessions s JOIN activities a ON a.id = s.activity_id WHERE a.name = 'Appendix work' LIMIT 1`).Scan(&sessionID); err != nil {
		t.Fatalf("find the session: %v", err)
	}
	var invoiceID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-SESSIONS-CSV', 'Client', '2026-10-20T00:00:00Z', '2026-10-27T00:00:00Z', 'sent', ?) RETURNING id`,
		teamID, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&invoiceID); err != nil {
		t.Fatalf("seed the invoice: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, sessionID); err != nil {
		t.Fatal(err)
	}

	response := e.do("GET", "/invoices/"+strconv.FormatInt(invoiceID, 10)+"/sessions.csv", nil, nil)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("the appendix answered %d as %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(body, "date,start,end,person,project,activity,hours,seconds\n") {
		t.Fatalf("the appendix header is wrong: %q", body)
	}
	if !strings.Contains(body, "2026-10-20,09:00,10:30,") || !strings.Contains(body, ",Appendix work,1.50,5400") {
		t.Fatalf("the appendix row is missing the session: %q", body)
	}
}
