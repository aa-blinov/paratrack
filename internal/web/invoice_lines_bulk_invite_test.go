package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestInvoiceManualLinesAndBulkInvites(t *testing.T) {
	e := newAPIEnv(t)
	e.register("freelance-lines@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"freelance"}, "from": {"welcome"}}, nil).Body.Close()

	// Create a project with a rate and log a session yesterday.
	projResp := e.do("POST", "/api/projects", url.Values{"name": {"Site"}, "color": {"#2563eb"}}, map[string]string{"Accept": "application/json"})
	projBody := readBody(t, projResp)
	var proj struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(projBody), &proj); err != nil {
		t.Fatalf("decode project: %v (%s)", err, projBody)
	}
	pid := strconv.FormatInt(proj.ID, 10)
	e.do("POST", "/projects/site/rate", url.Values{"hourly_rate": {"1000"}}, nil).Body.Close()
	e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Design"}, "project_id": {pid}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"},
	}, nil).Body.Close()

	// Create draft invoice.
	invResp := e.do("POST", "/invoices", url.Values{
		"client": {"Acme"}, "project_id": {pid}, "start": {"2020-01-01"}, "end": {"2030-01-01"}, "confirm_unapproved": {"1"},
	}, nil)
	invLoc := invResp.Header.Get("Location")
	invResp.Body.Close()
	if !strings.HasPrefix(invLoc, "/invoices/") {
		t.Fatalf("expected invoice redirect, got %q", invLoc)
	}

	// Add a manual extra line (+500).
	addResp := e.do("POST", invLoc+"/lines", url.Values{"label": {"Server setup"}, "amount": {"500"}}, nil)
	addResp.Body.Close()
	if addResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("add line status = %d", addResp.StatusCode)
	}

	page := readBody(t, e.do("GET", invLoc, nil, nil))
	detail := reactData[invoiceDetailPage](t, page)
	var extraLineID int64
	for _, l := range detail.Inv.Lines {
		if l.Label == "Server setup" && l.Extra {
			extraLineID = l.ID
		}
	}
	if extraLineID == 0 {
		t.Fatalf("expected extra line in invoice detail, got %+v", detail.Inv.Lines)
	}

	// Delete the manual extra line.
	delResp := e.do("POST", invLoc+"/lines/"+strconv.FormatInt(extraLineID, 10)+"/delete", nil, nil)
	delResp.Body.Close()
	page = readBody(t, e.do("GET", invLoc, nil, nil))
	detail = reactData[invoiceDetailPage](t, page)
	for _, l := range detail.Inv.Lines {
		if l.ID == extraLineID {
			t.Fatalf("expected extra line %d to be deleted", extraLineID)
		}
	}

	// A discount larger than the invoice is refused: two hours at 1000 make
	// 2000, and a -5000 typed with one zero too many would go out negative.
	tooBig := e.do("POST", invLoc+"/lines", url.Values{"label": {"Скидка"}, "amount": {"-5000"}}, nil)
	tooBig.Body.Close()
	if loc := tooBig.Header.Get("Location"); !strings.Contains(loc, "flash=") {
		t.Fatalf("oversized discount: expected a flash redirect, got %q", loc)
	}
	detail = reactData[invoiceDetailPage](t, readBody(t, e.do("GET", invLoc, nil, nil)))
	for _, l := range detail.Inv.Lines {
		if l.Label == "Скидка" {
			t.Fatalf("oversized discount was added: %+v", detail.Inv.Lines)
		}
	}

	// A draft with a negative total cannot be sent, however its lines got
	// there.
	invID := strings.TrimPrefix(invLoc, "/invoices/")
	if _, err := e.db.TestSQL().ExecContext(t.Context(),
		`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents) VALUES (?, 'Скидка', '', 0, 0, -1000000)`, invID); err != nil {
		t.Fatal(err)
	}
	e.do("POST", invLoc+"/status", url.Values{"status": {"sent"}}, nil).Body.Close()
	var status string
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT status FROM invoices WHERE id = ?`, invID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("an invoice with a negative total was sent: status %q", status)
	}

	// Bulk invites.
	bulkResp := e.do("POST", "/api/team/invites", url.Values{"emails": {"a@studio.test\nb@studio.test, invalid-email"}}, nil)
	bulkResp.Body.Close()
	invitesPageBody := readBody(t, e.do("GET", "/settings/invites", nil, nil))
	if !strings.Contains(invitesPageBody, "a@studio.test") && strings.Count(invitesPageBody, `"Live":true`) < 2 {
		t.Fatalf("expected 2 live invites after bulk create, got %s", invitesPageBody)
	}
}
