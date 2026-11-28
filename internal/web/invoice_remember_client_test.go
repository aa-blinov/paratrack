package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A new invoice remembers the client on the client card when the project has
// one, since the next invoice reads the card first; the project's own copy is
// left alone, and an empty field does not clear the card.
func TestNewInvoiceRemembersTheClientOnItsCard(t *testing.T) {
	e := newAPIEnv(t)
	e.register("remember-card@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"freelance"}, "from": {"welcome"}}, nil).Body.Close()
	projBody := readBody(t, e.do("POST", "/api/projects", url.Values{"name": {"Сайт"}, "color": {"#2563eb"}}, map[string]string{"Accept": "application/json"}))
	var proj struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(projBody), &proj); err != nil {
		t.Fatalf("decode project: %v (%s)", err, projBody)
	}
	pid := strconv.FormatInt(proj.ID, 10)
	e.do("POST", "/projects/sayt/rate", url.Values{"hourly_rate": {"1000"}}, nil).Body.Close()
	e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Дизайн"}, "project_id": {pid}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}}, nil).Body.Close()

	sql := e.db.TestSQL()
	ctx := t.Context()
	var clientID int64
	if err := sql.QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name, email, details) SELECT team_id, 'Ромашка', 'old@romashka.test', 'ИНН 7707083893' FROM projects WHERE id = ? RETURNING id`, proj.ID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := sql.ExecContext(ctx, `UPDATE projects SET client_id = ?, client_details = 'старая копия' WHERE id = ?`, clientID, proj.ID); err != nil {
		t.Fatal(err)
	}

	resp := e.do("POST", "/invoices", url.Values{
		"client": {"Ромашка"}, "project_id": {pid}, "start": {"2020-01-01"}, "end": {"2030-01-01"}, "confirm_unapproved": {"1"},
		"client_details": {"ИНН 500100732259"}, "client_email": {""},
	}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("create invoice: %d %q", resp.StatusCode, loc)
	}
	var email, details, copyDetails string
	if err := sql.QueryRowContext(ctx, `SELECT email, details FROM clients WHERE id = ?`, clientID).Scan(&email, &details); err != nil {
		t.Fatal(err)
	}
	if err := sql.QueryRowContext(ctx, `SELECT client_details FROM projects WHERE id = ?`, proj.ID).Scan(&copyDetails); err != nil {
		t.Fatal(err)
	}
	if details != "ИНН 500100732259" || email != "old@romashka.test" || copyDetails != "старая копия" {
		t.Fatalf("card %q %q, project copy %q", email, details, copyDetails)
	}
}
