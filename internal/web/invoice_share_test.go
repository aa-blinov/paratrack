package web

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A manager makes a client's link; the client opens the PDF with no account, and a
// wrong or expired token is not found (FIN-13).
func TestClientOpensTheInvoiceWithoutAnAccount(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-share@x.test")
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT team_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("find the workspace: %v", err)
	}
	var invoiceID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-LINK', 'Client', '2026-10-01T00:00:00Z', '2026-10-08T00:00:00Z', 'sent', '2026-10-08T09:00:00Z') RETURNING id`,
		teamID).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}

	made := e.do("POST", "/api/invoices/"+strconv.FormatInt(invoiceID, 10)+"/share", url.Values{}, map[string]string{"Accept": "application/json"})
	body := readBody(t, made)
	if made.StatusCode != http.StatusOK || !strings.Contains(body, "/share/invoices/") || !strings.Contains(body, ".pdf") {
		t.Fatalf("make the link: status %d, body %s", made.StatusCode, body)
	}
	start := strings.Index(body, "/share/invoices/") + len("/share/invoices/")
	end := strings.Index(body[start:], ".pdf")
	token := body[start : start+end]

	// No cookies: the client has no account.
	anonymous := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	open, err := anonymous.Get(e.ts.URL + "/share/invoices/" + token + ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(open.Body)
	open.Body.Close()
	if open.StatusCode != http.StatusOK || open.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("the client's link answered %d as %q", open.StatusCode, open.Header.Get("Content-Type"))
	}
	if open.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the client's PDF may be cached: %q", open.Header.Get("Cache-Control"))
	}

	wrong, err := anonymous.Get(e.ts.URL + "/share/invoices/not-a-token.pdf")
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusNotFound {
		t.Fatalf("a wrong token answered %d, want 404", wrong.StatusCode)
	}

	if _, err := e.db.TestSQL().ExecContext(t.Context(),
		`UPDATE invoice_share_links SET expires_at = ? WHERE invoice_id = ?`,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), invoiceID); err != nil {
		t.Fatal(err)
	}
	expired, err := anonymous.Get(e.ts.URL + "/share/invoices/" + token + ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	expired.Body.Close()
	if expired.StatusCode != http.StatusNotFound {
		t.Fatalf("an expired link answered %d, want 404", expired.StatusCode)
	}
}
