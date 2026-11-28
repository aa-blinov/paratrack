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

	// A client number that does not check out never gets into a draft, so a
	// client link made earlier cannot show it.
	editResp := e.do("POST", invLoc+"/edit", url.Values{"client": {"Acme"}, "client_details": {"ООО Acme, ИНН 7707083894"}}, nil)
	editResp.Body.Close()
	if loc := editResp.Header.Get("Location"); !strings.Contains(loc, "flash=") || strings.Contains(loc, "flash=updated") {
		t.Fatalf("a wrong client ИНН was saved: redirect %q", loc)
	}
	var clientDetails string
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT client_details FROM invoices WHERE id = ?`, invID).Scan(&clientDetails); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clientDetails, "7707083894") {
		t.Fatalf("client details saved: %q", clientDetails)
	}
	// A sole trader's pair with no КПП is fine.
	okResp := e.do("POST", invLoc+"/edit", url.Values{"client": {"Acme"}, "client_details": {"ИП Петров, ИНН/КПП 500100732259/"}}, nil)
	okResp.Body.Close()
	if loc := okResp.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("a valid sole trader pair was refused: %q", loc)
	}

	// "Update the client card too" on a project with no card says so and
	// saves nothing, rather than report a save that skipped the card.
	noCard := e.do("POST", invLoc+"/edit", url.Values{"client": {"Acme"}, "client_details": {"ООО Новая"}, "update_client_card": {"1"}}, nil)
	noCard.Body.Close()
	if loc := noCard.Header.Get("Location"); strings.Contains(loc, "flash=updated") {
		t.Fatalf("a card update with no card reported success: %q", loc)
	}
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT client_details FROM invoices WHERE id = ?`, invID).Scan(&clientDetails); err != nil {
		t.Fatal(err)
	}
	if clientDetails == "ООО Новая" {
		t.Fatal("the draft was saved although the card update was refused")
	}
	// An email that is not an address is refused before anything is written.
	badMail := e.do("POST", invLoc+"/edit", url.Values{"client": {"Acme"}, "client_email": {"not-an-address"}}, nil)
	badMail.Body.Close()
	if loc := badMail.Header.Get("Location"); strings.Contains(loc, "flash=updated") {
		t.Fatalf("a malformed email was saved: %q", loc)
	}
	// A draft saved before the check, with a wrong number in it, can still
	// get a new due date while its details stay as they are.
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE invoices SET client_details = 'ИНН 7707083894' WHERE id = ?`, invID); err != nil {
		t.Fatal(err)
	}
	legacy := e.do("POST", invLoc+"/edit", url.Values{"client": {"Acme"}, "client_details": {"ИНН 7707083894"}, "notes": {"новый срок"}}, nil)
	legacy.Body.Close()
	if loc := legacy.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("a legacy draft could not be edited with its details unchanged: %q", loc)
	}
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE invoices SET client_details = '' WHERE id = ?`, invID); err != nil {
		t.Fatal(err)
	}

	// The client card refuses a wrong number too, at create and at update.
	cardResp := e.do("POST", "/clients", url.Values{"name": {"Бета"}, "details": {"ИНН 7707083894"}}, nil)
	cardResp.Body.Close()
	var cards int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM clients WHERE name = 'Бета'`).Scan(&cards); err != nil {
		t.Fatal(err)
	}
	if cards != 0 {
		t.Fatal("a client card with a wrong ИНН was created")
	}

	// Details typed by hand are checked on the server too: a wrong ИНН is
	// not saved, whatever the browser let through.
	reqResp := e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП Иванов\nИНН 7707083894"}}, nil)
	reqResp.Body.Close()
	if loc := reqResp.Header.Get("Location"); !strings.Contains(loc, "flash=") || strings.Contains(loc, "flash=updated") {
		t.Fatalf("a wrong ИНН was saved: redirect %q", loc)
	}
	settings := readBody(t, e.do("GET", "/settings/money", nil, nil))
	if strings.Contains(settings, "7707083894") {
		t.Fatal("the wrong ИНН reached the saved details")
	}

	// Bulk invites.
	bulkResp := e.do("POST", "/api/team/invites", url.Values{"emails": {"a@studio.test\nb@studio.test, invalid-email"}}, nil)
	bulkResp.Body.Close()
	invitesPageBody := readBody(t, e.do("GET", "/settings/invites", nil, nil))
	if !strings.Contains(invitesPageBody, "a@studio.test") && strings.Count(invitesPageBody, `"Live":true`) < 2 {
		t.Fatalf("expected 2 live invites after bulk create, got %s", invitesPageBody)
	}
}
