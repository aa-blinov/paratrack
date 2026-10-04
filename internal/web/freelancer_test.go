package web

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/mail"
)

type captureMail struct {
	mu                sync.Mutex
	to, subject, html string
	files             []mail.Attachment
}

func (c *captureMail) Send(ctx context.Context, to, subject, body string) error {
	return c.Deliver(ctx, mail.Message{To: to, Subject: subject, Text: body})
}
func (c *captureMail) Deliver(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.to, c.subject, c.html, c.files = m.To, m.Subject, m.HTML, m.Files
	return nil
}

func (c *captureMail) snapshot() (string, string, []mail.Attachment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.to, c.html, append([]mail.Attachment(nil), c.files...)
}

// The freelancer loop: unbilled → immutable invoice snapshot → queued mail →
// sent document with its ledger locked.
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

	// Rebuild the draft from the edited ledger before sending it.
	resp := e.do("PATCH", "/api/sessions/1", url.Values{"duration": {"3h"}}, htmx)
	resp.Body.Close()
	resp = e.do("POST", first.loc+"/rebuild", nil, nil)
	resp.Body.Close()
	if page := readBody(t, e.do("GET", first.loc, nil, nil)); !strings.Contains(page, "9 000,00") {
		t.Fatal("draft rebuild didn't pick up the edited hours")
	}

	// Send it (mail is queued durably and captured by a worker).
	t.Setenv("PARATRACK_SMTP_HOST", "smtp.test:25")
	cm := &captureMail{}
	e.srv.runtime.Mailer = cm
	stopWorker := e.srv.startMailWorker(context.Background())
	defer stopWorker()
	resp = e.do("POST", first.loc+"/send", url.Values{"to": {"buh@romashka.ru"}}, nil)
	resp.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	var to, letter string
	var files []mail.Attachment
	var page string
	for time.Now().Before(deadline) {
		to, letter, files = cm.snapshot()
		page = readBody(t, e.do("GET", first.loc, nil, nil))
		if to != "" && strings.Contains(page, "doc-status is-sent") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if to != "buh@romashka.ru" || len(files) != 1 || !strings.HasPrefix(string(files[0].Data), "%PDF") {
		t.Fatalf("mail not sent with the PDF: to %q files %d", to, len(files))
	}
	if !strings.Contains(letter, "К оплате") || !strings.Contains(letter, "9\u00a0000,00") && !strings.Contains(letter, "6\u00a0000,00") {
		t.Errorf("HTML letter lacks the amount block")
	}
	if !strings.Contains(page, "doc-status is-sent") {
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
	// A sent invoice is immutable and cannot be deleted to unlock its ledger.
	resp = e.do("POST", first.loc+"/delete", nil, nil)
	resp.Body.Close()
	resp = e.do("PATCH", "/api/sessions/1", url.Values{"duration": {"3h"}}, htmx)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("sent invoice allowed ledger edit after attempted deletion: %d", resp.StatusCode)
	}
}

type httpResult struct {
	code int
	loc  string
}
