package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// issuedInvoiceEnv makes a workspace with one invoice and returns the env,
// the workspace, the owner and the invoice.
func issuedInvoiceEnv(t *testing.T, email string) (*apiEnv, int64, int64, int64) {
	t.Helper()
	e := newAPIEnv(t)
	e.register(email)
	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT team_id, user_id FROM memberships WHERE role = 'owner' ORDER BY team_id DESC LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-901", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0),
		Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 3600, RateCents: 100000, AmountCents: 100000}}})
	if err != nil {
		t.Fatal(err)
	}
	return e, teamID, ownerID, inv.ID
}

// A sent invoice keeps the seller name it went out with and is dated by the
// day it was sent; a client link is refused on a draft.
func TestSentInvoiceKeepsItsIssuedLook(t *testing.T) {
	e, teamID, _, id := issuedInvoiceEnv(t, "issued-look@x.test")
	json := map[string]string{"Accept": "application/json"}
	resp := e.do("POST", "/api/invoices/"+itoa64(id)+"/share", url.Values{}, json)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a client link on a draft: %d, want 409", resp.StatusCode)
	}
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE invoices SET created_at = '2026-06-03T09:00:00Z' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/status", url.Values{"status": {"sent"}}, nil).Body.Close()
	var raw string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT issued_snapshot FROM invoices WHERE id = ?`, id).Scan(&raw)
	var issued invoiceIssued
	if err := jsonUnmarshal(raw, &issued); err != nil || issued.Seller == "" || issued.IssuedOn == "2026-06-03" {
		t.Fatalf("the issued look was not kept at send: %q %v", raw, err)
	}
	seller := issued.Seller
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE users SET name = 'Новое имя' WHERE id IN (SELECT owner_id FROM teams WHERE id = ?)`, teamID); err != nil {
		t.Fatal(err)
	}
	page := readBody(t, e.do("GET", "/invoices/"+itoa64(id)+"/act", nil, nil))
	// The shell names the signed-in person anyway; the act's own seller is the kept one.
	if strings.Contains(page, `"Seller":"Новое имя"`) || !strings.Contains(page, `"Seller":"`+seller+`"`) {
		t.Fatalf("the issued act follows the renamed seller (kept %q)", seller)
	}
}

// A sent invoice with nothing paid is voided with a reason: its hours come
// back, payments are refused, and its client link says it was voided.
func TestVoidedInvoiceGivesItsHoursBackAndTellsTheClient(t *testing.T) {
	e, _, ownerID, id := issuedInvoiceEnv(t, "void@x.test")
	sql := e.db.TestSQL()
	if _, err := sql.ExecContext(t.Context(),
		`INSERT INTO sessions (team_id, user_id, activity_id, start_at, end_at, invoice_id, note) SELECT team_id, ?, (SELECT id FROM activities WHERE team_id = invoices.team_id LIMIT 1), '2026-06-02T09:00:00Z', '2026-06-02T10:00:00Z', id, '' FROM invoices WHERE id = ? AND EXISTS (SELECT 1 FROM activities WHERE team_id = invoices.team_id)`, ownerID, id); err != nil {
		t.Logf("no session seeded: %v", err)
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/status", url.Values{"status": {"sent"}}, nil).Body.Close()
	body := readBody(t, e.do("POST", "/api/invoices/"+itoa64(id)+"/share", url.Values{}, map[string]string{"Accept": "application/json"}))
	var link struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &link); err != nil || link.URL == "" {
		t.Fatalf("share a sent invoice: %v %s", err, body)
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/void", url.Values{}, nil).Body.Close()
	status := func() string {
		var s string
		sql.QueryRowContext(t.Context(), `SELECT status FROM invoices WHERE id = ?`, id).Scan(&s)
		return s
	}
	if status() != "sent" {
		t.Fatal("voided without a reason")
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/void", url.Values{"reason": {"выставлен не тому юрлицу"}}, nil).Body.Close()
	if status() != "void" {
		t.Fatalf("not voided: %s", status())
	}
	var billed int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sessions WHERE invoice_id = ?`, id).Scan(&billed)
	if billed != 0 {
		t.Fatalf("the voided invoice kept %d sessions", billed)
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/payments", url.Values{"amount": {"100"}, "paid_on": {"2026-07-01"}}, nil).Body.Close()
	var payments int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, id).Scan(&payments)
	if payments != 0 {
		t.Fatal("a payment was booked on a voided invoice")
	}
	shared := e.do("GET", link.URL[strings.Index(link.URL, "/share/"):], nil, nil)
	text := readBody(t, shared)
	if shared.StatusCode != http.StatusGone || !strings.Contains(text, "INV-2026-901") || !strings.Contains(text, "не тому юрлицу") {
		t.Fatalf("the client link of a voided invoice: %d %q", shared.StatusCode, text)
	}
}

func jsonUnmarshal(raw string, v any) error { return json.Unmarshal([]byte(raw), v) }

// The accountant's register dates an invoice by the day it was issued, not
// the day its draft was made, and the letter file keeps that day.
func TestRegisterAndLetterUseTheIssueDay(t *testing.T) {
	e, _, _, id := issuedInvoiceEnv(t, "issue-day@x.test")
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE invoices SET created_at = '2026-06-03T09:00:00Z' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	letter := e.do("GET", "/invoices/"+itoa64(id)+"/letter.eml", nil, nil)
	body := readBody(t, letter)
	if letter.StatusCode != 200 || (!strings.Contains(body, "act-INV-2026-901.pdf") || !strings.Contains(body, "INV-2026-901-sessions.csv")) {
		t.Fatalf("the letter file (%d) lacks the act or the breakdown", letter.StatusCode)
	}
	e.do("POST", "/invoices/"+itoa64(id)+"/status", url.Values{"status": {"sent"}}, nil).Body.Close()
	var issuedOn string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT issued_on FROM invoices WHERE id = ?`, id).Scan(&issuedOn)
	today := time.Now().UTC().Format("2006-01-02")
	if issuedOn == "" || issuedOn == "2026-06-03" {
		t.Fatalf("issued_on = %q, want the send day", issuedOn)
	}
	csv := readBody(t, e.do("GET", "/invoices/export.csv?export_from="+issuedOn+"&export_to="+issuedOn, nil, nil))
	if !strings.Contains(csv, "INV-2026-901,"+issuedOn) {
		t.Fatalf("the register (want %s, today %s): %q", issuedOn, today, csv)
	}
}
