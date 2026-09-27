package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/mail"
)

type captureMail struct {
	to, subject, html string
	files             []mail.Attachment
}

func (c *captureMail) Send(to, subject, body string) error {
	return c.Deliver(mail.Message{To: to, Subject: subject, Text: body})
}
func (c *captureMail) Deliver(m mail.Message) error {
	c.to, c.subject, c.html, c.files = m.To, m.Subject, m.HTML, m.Files
	return nil
}

// The whole freelancer loop: unbilled → invoice (client remembered) →
// billed once → locked after sending → released by deleting the invoice.
func TestFreelancerBillingLoop(t *testing.T) {
	e := newAPIEnv(t)
	e.register("free@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}, "rate": {"3000"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}, "project_id": {"1"}}, htmx))

	if dash := readBody(t, e.do("GET", "/", nil, nil)); !strings.Contains(dash, "6 000,00") {
		t.Fatal("dashboard doesn't show 6 000,00 not invoiced")
	}
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	mk := func() *httpResult {
		resp := e.do("POST", "/invoices", url.Values{"project_id": {"1"}, "client": {"ООО «Ромашка»"},
			"client_email": {"buh@romashka.ru"}, "start": {day}, "end": {day}}, nil)
		defer resp.Body.Close()
		return &httpResult{resp.StatusCode, resp.Header.Get("Location")}
	}
	first := mk()
	if !strings.HasPrefix(first.loc, "/invoices/") || strings.Contains(first.loc, "flash") {
		t.Fatalf("first invoice: %+v", first)
	}
	if dash := readBody(t, e.do("GET", "/", nil, nil)); strings.Contains(dash, "6 000,00") {
		t.Error("billed time still shown as not invoiced")
	}
	if again := mk(); !strings.Contains(again.loc, "flash=") || strings.HasPrefix(again.loc, "/invoices/2") {
		t.Errorf("the same hours were billed twice: %+v", again)
	}
	if form := readBody(t, e.do("GET", "/invoices?project=1", nil, nil)); !strings.Contains(form, "buh@romashka.ru") {
		t.Error("client not remembered on the project")
	}

	// Send it (mail configured, captured).
	t.Setenv("PARATRACK_SMTP_HOST", "smtp.test:25")
	cm := &captureMail{}
	e.srv.mailer = cm
	resp := e.do("POST", first.loc+"/send", url.Values{"to": {"buh@romashka.ru"}}, nil)
	resp.Body.Close()
	if cm.to != "buh@romashka.ru" || len(cm.files) != 1 || !strings.HasPrefix(string(cm.files[0].Data), "%PDF") {
		t.Fatalf("mail not sent with the PDF: to %q files %d", cm.to, len(cm.files))
	}
	if !strings.Contains(cm.html, "К оплате") || !strings.Contains(cm.html, "9\u00a0000,00") && !strings.Contains(cm.html, "6\u00a0000,00") {
		t.Errorf("HTML letter lacks the amount block")
	}
	if page := readBody(t, e.do("GET", first.loc, nil, nil)); !strings.Contains(page, "doc-status is-sent") {
		t.Error("sending didn't mark the invoice sent")
	}

	// Sent → its hours are locked.
	resp = e.do("PATCH", "/api/sessions/1", url.Values{"duration": {"3h"}}, htmx)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("editing a billed session: %d, want 409", resp.StatusCode)
	}
	resp = e.do("DELETE", "/api/sessions/1", nil, htmx)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("deleting a billed session: %d, want 409", resp.StatusCode)
	}
	// Deleting the invoice releases them.
	resp = e.do("POST", first.loc+"/delete", nil, nil)
	resp.Body.Close()
	resp = e.do("PATCH", "/api/sessions/1", url.Values{"duration": {"3h"}}, htmx)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("after deleting the invoice the session is still locked: %d", resp.StatusCode)
	}

	// A draft rebuild picks the edit up: 3 h × 3000 = 9 000,00.
	second := mk()
	resp = e.do("PATCH", "/api/sessions/1", url.Values{"duration": {"4h"}}, htmx)
	resp.Body.Close()
	resp = e.do("POST", second.loc+"/rebuild", nil, nil)
	resp.Body.Close()
	if page := readBody(t, e.do("GET", second.loc, nil, nil)); !strings.Contains(page, "12 000,00") {
		t.Error("rebuild didn't pick up the edited hours (want 12 000,00)")
	}
}

type httpResult struct {
	code int
	loc  string
}
