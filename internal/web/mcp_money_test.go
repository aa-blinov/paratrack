package web

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// countRows reads one count from the test database.
func countRows(t *testing.T, e *apiEnv, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// invoiceMCPView is the part of an invoice answer the money tests read.
type invoiceMCPView struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	TotalCents int    `json:"total_cents"`
	PaidCents  int    `json:"paid_cents"`
	Settled    bool   `json:"settled"`
}

func decodeInvoiceMCP(t *testing.T, text string) invoiceMCPView {
	t.Helper()
	var view invoiceMCPView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("answer is not an invoice: %v %q", err, text)
	}
	return view
}

// readOnlyBearer mints a read-only API token for the workspace and returns its header.
func readOnlyBearer(t *testing.T, e *apiEnv) map[string]string {
	t.Helper()
	page := readBody(t, e.do("POST", "/api/tokens", url.Values{"name": {"reader"}, "read_only": {"1"}}, nil))
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	return mcpBearer(mustUnescape(t, raw))
}

// The owner drafts an invoice from billable time, issues it and records the two
// payments that settle it. Every step is the workflow the page calls, and the rows
// show the status each step leaves.
func TestMCPManagerDraftsIssuesAndPaysAnInvoice(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-money-owner@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	htmx := map[string]string{"HX-Request": "true"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}, "rate": {"3000"}}, nil))
	var projectID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects ORDER BY id DESC LIMIT 1`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"вёрстка"}, "start": {"2026-09-22 10:00"}, "end": {"2026-09-22 12:00"},
		"project_id": {strconv.FormatInt(projectID, 10)},
	}, htmx))
	draft := map[string]any{"start": "2026-09-22", "end": "2026-09-22", "project_id": projectID, "client": "ООО «Ромашка»"}

	if text, failed := callTool(t, e, auth, "invoice_draft_create", draft); !failed || !strings.Contains(text, "неутверждённые") {
		t.Fatalf("a draft over an unapproved week = %v %q, want the unapproved-weeks refusal", failed, text)
	}
	if text, failed := callTool(t, e, auth, "timesheet_week_approve", map[string]any{"week_start": "2026-09-21"}); failed {
		t.Fatalf("timesheet_week_approve: %s", text)
	}
	if text, failed := callTool(t, e, auth, "invoice_draft_create", map[string]any{"start": "2026-09-22", "end": "2026-09-22", "project_id": projectID}); !failed || !strings.Contains(text, "client") {
		t.Fatalf("a draft without a client or a project client = %v %q, want the client named", failed, text)
	}

	text, failed := callTool(t, e, auth, "invoice_draft_create", draft)
	if failed {
		t.Fatalf("invoice_draft_create: %s", text)
	}
	created := decodeInvoiceMCP(t, text)
	if created.Status != "draft" || created.TotalCents != 600000 {
		t.Fatalf("draft = %+v, want a draft of 6 000,00", created)
	}
	if status := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'draft'`, created.ID); status != 1 {
		t.Fatal("the draft is not in the database as a draft")
	}

	if text, failed := callTool(t, e, auth, "invoice_mark_sent", map[string]any{"invoice_id": created.ID}); failed || decodeInvoiceMCP(t, text).Status != "sent" {
		t.Fatalf("invoice_mark_sent = %v %q, want status sent", failed, text)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'sent'`, created.ID); n != 1 {
		t.Fatal("the issued invoice is not sent in the database")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM audit_log WHERE action = 'mcp.write' AND target = 'invoice_mark_sent'`); n != 1 {
		t.Fatalf("audit rows for invoice_mark_sent = %d, want 1", n)
	}

	partial := map[string]any{"invoice_id": created.ID, "amount_cents": 100000, "paid_on": "2026-10-01"}
	if text, failed := callTool(t, e, auth, "invoice_record_payment", partial); failed {
		t.Fatalf("partial payment: %s", text)
	} else if view := decodeInvoiceMCP(t, text); view.Settled || view.Status != "sent" || view.PaidCents != 100000 {
		t.Fatalf("partial payment = %+v, want the invoice still sent with 1 000,00 paid", view)
	}
	if text, failed := callTool(t, e, auth, "invoice_record_payment", map[string]any{"invoice_id": created.ID, "amount_cents": 500001, "paid_on": "2026-10-02"}); !failed || !strings.Contains(text, "больше того") {
		t.Fatalf("an overpayment = %v %q, want the amount refused", failed, text)
	}

	text, failed = callTool(t, e, auth, "invoice_record_payment", map[string]any{"invoice_id": created.ID, "amount_cents": 500000, "paid_on": "2026-10-02", "note": "перевод"})
	if failed {
		t.Fatalf("final payment: %s", text)
	}
	if view := decodeInvoiceMCP(t, text); !view.Settled || view.Status != "paid" {
		t.Fatalf("final payment = %+v, want settled and paid", view)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'paid'`, created.ID); n != 1 {
		t.Fatal("the settled invoice is not paid in the database")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, created.ID); n != 2 {
		t.Fatalf("payments in the journal = %d, want 2", n)
	}
}

// The owner pays a member for a week, drafts a pay run for it, refuses a second run
// over the same days, and pays the run.
func TestMCPManagerRunsPayrollAndApprovesAWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-money-pay@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	htmx := map[string]string{"HX-Request": "true"}
	var ownerID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'mcp-money-pay@x.test'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	resp := e.do("POST", "/api/member/pay", url.Values{"user_id": {strconv.FormatInt(ownerID, 10)}, "hourly_pay": {"1000"}}, nil)
	resp.Body.Close()
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"2026-09-22 10:00"}, "end": {"2026-09-22 12:00"}}, htmx))

	if text, failed := callTool(t, e, auth, "timesheet_week_approve", map[string]any{"week_start": "2026-09-23"}); !failed || !strings.Contains(text, "week_start") {
		t.Fatalf("a day in the middle of a week = %v %q, want week_start named", failed, text)
	}
	if text, failed := callTool(t, e, auth, "timesheet_week_approve", map[string]any{"week_start": "2026-09-21"}); failed || !strings.Contains(text, `"approved":true`) {
		t.Fatalf("timesheet_week_approve = %v %q", failed, text)
	}

	run := map[string]any{"start": "2026-09-22", "end": "2026-09-22", "notes": "сентябрь"}
	text, failed := callTool(t, e, auth, "payroll_run_create", run)
	if failed {
		t.Fatalf("payroll_run_create: %s", text)
	}
	var created struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		TotalCents int    `json:"total_cents"`
	}
	if err := json.Unmarshal([]byte(text), &created); err != nil {
		t.Fatalf("answer is not a pay run: %v %q", err, text)
	}
	if created.Status != "draft" || created.TotalCents <= 0 {
		t.Fatalf("pay run = %+v, want a draft with a total", created)
	}
	if text, failed := callTool(t, e, auth, "payroll_run_create", run); !failed || !strings.Contains(text, "пересекается") {
		t.Fatalf("an overlapping run = %v %q, want the overlap refusal", failed, text)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM payroll_runs`); n != 1 {
		t.Fatalf("pay runs after an overlap refusal = %d, want 1", n)
	}

	if text, failed := callTool(t, e, auth, "payroll_run_pay", map[string]any{"run_id": created.ID}); failed || !strings.Contains(text, `"status":"paid"`) {
		t.Fatalf("payroll_run_pay = %v %q", failed, text)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM payroll_runs WHERE id = ? AND status = 'paid'`, created.ID); n != 1 {
		t.Fatal("the paid run is not paid in the database")
	}
}

// A member's token is refused every money write, and nothing changes in the database.
func TestMCPMemberIsRefusedEveryMoneyWrite(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "money")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	var studioID, ownerID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams WHERE name = 'Студия' ORDER BY id DESC LIMIT 1`).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT user_id FROM memberships WHERE team_id = ? AND role = 'owner' LIMIT 1`, studioID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	inv, err := owner.db.CreateInvoice(requestctx.WithActor(t.Context(), ownerID), appmodel.InvoiceCreateRequest{
		TeamID: studioID, CallerID: ownerID, Number: "INV-MCP-MEMBER", Client: "Acme", Start: start, End: start.AddDate(0, 0, 1),
		Lines: []model.InvoiceLine{{Label: "Acme · consulting", Seconds: 3600, RateCents: 10000, AmountCents: 10000}},
	})
	if err != nil {
		t.Fatalf("seed the invoice: %v", err)
	}

	refused := []struct {
		name string
		args map[string]any
	}{
		{"invoice_draft_create", map[string]any{"start": "2026-09-22", "end": "2026-09-22", "client": "Acme"}},
		{"invoice_mark_sent", map[string]any{"invoice_id": inv.ID}},
		{"invoice_send", map[string]any{"invoice_id": inv.ID, "email": "buh@romashka.ru"}},
		{"invoice_record_payment", map[string]any{"invoice_id": inv.ID, "amount_cents": 100, "paid_on": "2026-10-01"}},
		{"payroll_run_create", map[string]any{"start": "2026-09-22", "end": "2026-09-22"}},
		{"payroll_run_pay", map[string]any{"run_id": 1}},
		{"timesheet_week_approve", map[string]any{"week_start": "2026-09-21"}},
	}
	for _, tc := range refused {
		text, failed := callTool(t, member, memberAuth, tc.name, tc.args)
		if !failed || !strings.Contains(text, "прав") {
			t.Fatalf("a member called %s: failed=%v text=%q, want the forbidden text", tc.name, failed, text)
		}
	}

	if n := countRows(t, owner, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'draft'`, inv.ID); n != 1 {
		t.Fatal("a member's call changed the invoice status")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM invoices WHERE team_id = ?`, studioID); n != 1 {
		t.Fatalf("invoices in the workspace = %d, want the one seeded", n)
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, inv.ID); n != 0 {
		t.Fatal("a member's call booked a payment")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM invoice_email_outbox WHERE invoice_id = ?`, inv.ID); n != 0 {
		t.Fatal("a member's call queued a letter")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM payroll_runs WHERE team_id = ?`, studioID); n != 0 {
		t.Fatal("a member's call created a pay run")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ?`, studioID); n != 0 {
		t.Fatal("a member's call approved a week")
	}
}

// A read-only token reads invoices and is refused every write, issuing included.
func TestMCPReadOnlyTokenCannotIssueAnInvoice(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-money-ro@x.test")
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-MCP-RO")
	auth := readOnlyBearer(t, e)

	if text, failed := callTool(t, e, auth, "invoice_get", map[string]any{"invoice_id": invoiceID}); failed {
		t.Fatalf("a read-only token could not read the invoice: %s", text)
	}
	text, failed := callTool(t, e, auth, "invoice_mark_sent", map[string]any{"invoice_id": invoiceID})
	if !failed || !strings.Contains(text, "только для чтения") {
		t.Fatalf("invoice_mark_sent by a read-only token = %v %q, want the read-only refusal", failed, text)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'draft'`, invoiceID); n != 1 {
		t.Fatal("a read-only token changed the invoice")
	}
}

// Sending queues one letter for the invoice. No mail worker runs in the test, so the
// letter stays in the queue and nothing reaches a mail provider.
func TestMCPOwnerSendsAnInvoiceThroughTheMailQueue(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-money-send@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	e.srv.runtime.Mailer = &spyMail{}
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-MCP-SEND")

	if text, failed := callTool(t, e, auth, "invoice_send", map[string]any{"invoice_id": invoiceID, "email": "не адрес"}); !failed || !strings.Contains(text, "email") {
		t.Fatalf("a bad address = %v %q, want the email field named", failed, text)
	}
	text, failed := callTool(t, e, auth, "invoice_send", map[string]any{"invoice_id": invoiceID, "email": "buh@romashka.ru"})
	if failed {
		t.Fatalf("invoice_send: %s", text)
	}
	if !strings.Contains(text, `"status":"sending"`) || !strings.Contains(text, `"recipient":"buh@romashka.ru"`) {
		t.Fatalf("invoice_send answer = %q, want status sending and the recipient", text)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoice_email_outbox WHERE invoice_id = ? AND recipient = ? AND status = 'pending'`, invoiceID, "buh@romashka.ru"); n != 1 {
		t.Fatalf("letters in the mail queue = %d, want 1", n)
	}
	if _, failed := callTool(t, e, auth, "invoice_send", map[string]any{"invoice_id": invoiceID, "email": "buh@romashka.ru"}); !failed {
		t.Fatal("a second send of the same invoice was accepted")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoice_email_outbox WHERE invoice_id = ?`, invoiceID); n != 1 {
		t.Fatalf("letters after a refused second send = %d, want 1", n)
	}
}

// A draft with no client named takes the client of the project it is drafted for, as the
// page prefills it. The week the time sits in is approved first, as the draft requires.
func TestMCPDraftTakesTheProjectsClientByDefault(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-draft-client@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	clientID := insertTestClient(t, e, "Ромашка")

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Сайт"}, "rate": {"3000"}}, nil))
	var projectID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects ORDER BY id DESC LIMIT 1`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE projects SET client_id = ? WHERE id = ?`, clientID, projectID); err != nil {
		t.Fatal(err)
	}
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"вёрстка"}, "start": {"2026-09-22 10:00"}, "end": {"2026-09-22 12:00"},
		"project_id": {strconv.FormatInt(projectID, 10)},
	}, map[string]string{"HX-Request": "true"}))
	if text, failed := callTool(t, e, auth, "timesheet_week_approve", map[string]any{"week_start": "2026-09-21"}); failed {
		t.Fatalf("approve the week: %s", text)
	}

	text, failed := callTool(t, e, auth, "invoice_draft_create", map[string]any{"start": "2026-09-22", "end": "2026-09-22", "project_id": projectID})
	if failed || !strings.Contains(text, `"client_name":"Ромашка"`) {
		t.Fatalf("draft without a client = %v %q, want the project's client", failed, text)
	}
}

// Without an SMTP host the send is refused in words the page uses, and nothing changes: the
// invoice stays a draft and the outbox stays empty.
func TestMCPInvoiceSendIsRefusedWithoutMail(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-no-mail@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-NO-MAIL")
	e.srv.runtime.Mailer = nil

	text, failed := callTool(t, e, auth, "invoice_send", map[string]any{"invoice_id": invoiceID, "email": "buh@acme.test"})
	if !failed || text != i18n.T(i18n.Ru, "inv.mailOff") {
		t.Fatalf("send without SMTP = %v %q, want the page's message", failed, text)
	}
	if status := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'draft'`, invoiceID); status != 1 {
		t.Fatal("the invoice left draft although the send was refused")
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoice_email_outbox`); n != 0 {
		t.Fatalf("a refused send queued %d letters", n)
	}
}
