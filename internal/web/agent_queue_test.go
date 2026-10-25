package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// An agent asks for an action through the MCP tools, and a person confirms or rejects the
// request in the app. These tests walk that path: the request, the refusals and the decision.

// queueActionID reads the id out of a request's answer, which must be a pending request.
func queueActionID(t *testing.T, text string) int64 {
	t.Helper()
	var answer struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	mustDecode(t, text, &answer)
	if answer.ID <= 0 || answer.Status != "pending" {
		t.Fatalf("request answer %q, want a pending request with an id", text)
	}
	return answer.ID
}

// queueStatus reads a request's status and the text its outcome left.
func queueStatus(t *testing.T, e *apiEnv, id int64) (string, string) {
	t.Helper()
	var status, outcome string
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT status, error FROM agent_actions WHERE id = ?`, id).Scan(&status, &outcome); err != nil {
		t.Fatalf("read request %d: %v", id, err)
	}
	return status, outcome
}

// decideQueued posts the confirm or reject form of one request, as the page does.
func decideQueued(e *apiEnv, id int64, verb string, hdr map[string]string) *http.Response {
	return e.do("POST", fmt.Sprintf("/api/agent-actions/%d/%s", id, verb), nil, hdr)
}

func TestAgentRequestQueuesAnInvoiceDeletion(t *testing.T) {
	e := newAPIEnv(t)
	e.register("queue-request@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, teamID := seedInvoiceForMCP(t, e, "INV-QUEUE-A")

	text, failed := callTool(t, e, auth, "invoice_delete_request", map[string]any{"invoice_id": invoiceID, "reason": "Дубль счёта"})
	if failed {
		t.Fatalf("invoice_delete_request refused: %q", text)
	}
	queueActionID(t, text)
	if n := countRows(t, e, `SELECT COUNT(*) FROM agent_actions WHERE team_id = ? AND kind = 'invoice_delete' AND status = 'pending'`, teamID); n != 1 {
		t.Fatalf("pending invoice deletions = %d, want 1", n)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ?`, invoiceID); n != 1 {
		t.Fatal("asking for the deletion deleted the invoice")
	}
}

func TestAgentRequestsAreRefusedToMembers(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("queue-member-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("queue-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"queue-member@x.test": member}, "queue-member@x.test")
	auth := mcpBearer(apiTokenOf(t, member, "member"))

	requests := []struct {
		name string
		args map[string]any
	}{
		{"invoice_delete_request", map[string]any{"invoice_id": 1, "reason": "дубль"}},
		{"payroll_run_delete_request", map[string]any{"run_id": 1, "reason": "дубль"}},
		{"week_reopen_request", map[string]any{"week_start": "2020-03-02", "reason": "правка"}},
		{"workspace_delete_request", map[string]any{"reason": "конец"}},
	}
	for _, request := range requests {
		text, failed := callTool(t, member, auth, request.name, request.args)
		if !failed || !strings.Contains(text, "прав") {
			t.Fatalf("a member asked for %s: failed=%v text=%q", request.name, failed, text)
		}
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM agent_actions`); n != 0 {
		t.Fatalf("refused requests left %d rows", n)
	}
}

func TestAgentConfirmedInvoiceDeletionRunsOnce(t *testing.T) {
	e := newAPIEnv(t)
	e.register("queue-confirm@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-QUEUE-C")
	text, _ := callTool(t, e, auth, "invoice_delete_request", map[string]any{"invoice_id": invoiceID, "reason": "Дубль счёта"})
	id := queueActionID(t, text)

	resp := decideQueued(e, id, "confirm", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("confirm: status %d, want a redirect back to the queue", resp.StatusCode)
	}
	if status, outcome := queueStatus(t, e, id); status != "done" || outcome != "" {
		t.Fatalf("confirmed request: status %q error %q, want done", status, outcome)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ?`, invoiceID); n != 0 {
		t.Fatal("the confirmed request did not delete the invoice")
	}
}

func TestAgentRejectedRequestRunsNothing(t *testing.T) {
	e := newAPIEnv(t)
	e.register("queue-reject@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-QUEUE-D")
	text, _ := callTool(t, e, auth, "invoice_delete_request", map[string]any{"invoice_id": invoiceID, "reason": "Дубль счёта"})
	id := queueActionID(t, text)

	resp := decideQueued(e, id, "reject", nil)
	resp.Body.Close()
	if status, _ := queueStatus(t, e, id); status != "rejected" {
		t.Fatalf("rejected request: status %q, want rejected", status)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ?`, invoiceID); n != 1 {
		t.Fatal("rejecting the request deleted the invoice")
	}
	// A decided request stays decided: a later confirmation cannot run it.
	resp = decideQueued(e, id, "confirm", nil)
	resp.Body.Close()
	if status, _ := queueStatus(t, e, id); status != "rejected" {
		t.Fatalf("a rejected request was confirmed afterwards: status %q", status)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ?`, invoiceID); n != 1 {
		t.Fatal("a confirmation after the rejection deleted the invoice")
	}
}

func TestAgentWeekReopenWaitsForTheOwner(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("queue-week-owner@x.test")
	admin := newAPIEnvSharedDB(t, owner)
	admin.register("queue-week-admin@x.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"queue-week-admin@x.test": admin}, "queue-week-admin@x.test")
	resp := owner.do("POST", "/api/team/members/"+ids[0]+"/role", url.Values{"role": {"admin"}}, nil)
	resp.Body.Close()
	var teamID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams WHERE name = 'Студия' ORDER BY id DESC LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("load the shared workspace: %v", err)
	}

	// The owner approves a past week, as the timesheet does.
	resp = owner.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil)
	resp.Body.Close()
	if n := countRows(t, owner, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ?`, teamID); n != 1 {
		t.Fatalf("approved weeks = %d, want 1", n)
	}

	text, failed := callTool(t, admin, mcpBearer(apiTokenOf(t, admin, "agent")), "week_reopen_request",
		map[string]any{"week_start": "2020-03-02", "reason": "Ошибка в часах"})
	if failed {
		t.Fatalf("week_reopen_request refused: %q", text)
	}
	id := queueActionID(t, text)
	if n := countRows(t, owner, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ?`, teamID); n != 1 {
		t.Fatal("asking for the reopening reopened the week")
	}

	resp = decideQueued(owner, id, "confirm", nil)
	resp.Body.Close()
	if n := countRows(t, owner, `SELECT COUNT(*) FROM timesheet_approvals WHERE team_id = ?`, teamID); n != 0 {
		t.Fatal("the owner's confirmation did not reopen the week")
	}
	if status, _ := queueStatus(t, owner, id); status != "done" {
		t.Fatalf("reopened week: request status %q, want done", status)
	}
}

func TestAgentAdminCannotConfirmWorkspaceDeletion(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("queue-ws-owner@x.test")
	admin := newAPIEnvSharedDB(t, owner)
	admin.register("queue-ws-admin@x.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"queue-ws-admin@x.test": admin}, "queue-ws-admin@x.test")
	resp := owner.do("POST", "/api/team/members/"+ids[0]+"/role", url.Values{"role": {"admin"}}, nil)
	resp.Body.Close()
	var teamID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams WHERE name = 'Студия' ORDER BY id DESC LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("load the shared workspace: %v", err)
	}

	text, failed := callTool(t, owner, mcpBearer(apiTokenOf(t, owner, "agent")), "workspace_delete_request", map[string]any{"reason": "Студия закрыта"})
	if failed {
		t.Fatalf("workspace_delete_request refused for the owner: %q", text)
	}
	id := queueActionID(t, text)

	resp = decideQueued(admin, id, "confirm", nil)
	resp.Body.Close()
	if status, _ := queueStatus(t, owner, id); status != "pending" {
		t.Fatalf("an admin's confirmation changed the request to %q, want pending", status)
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM teams WHERE id = ?`, teamID); n != 1 {
		t.Fatal("an admin deleted the workspace")
	}
}

func TestAgentTokenCannotConfirmItsOwnRequest(t *testing.T) {
	e := newAPIEnv(t)
	e.register("queue-token@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, _ := seedInvoiceForMCP(t, e, "INV-QUEUE-G")
	text, _ := callTool(t, e, auth, "invoice_delete_request", map[string]any{"invoice_id": invoiceID, "reason": "Дубль счёта"})
	id := queueActionID(t, text)

	// The agent holds the owner's token. Its own request stays its to ask for, not to confirm.
	for _, verb := range []string{"confirm", "reject"} {
		resp := decideQueued(e, id, verb, auth)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s with an API token: status %d, want 403", verb, resp.StatusCode)
		}
	}
	if status, _ := queueStatus(t, e, id); status != "pending" {
		t.Fatalf("a token decided its own request: status %q", status)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM invoices WHERE id = ?`, invoiceID); n != 1 {
		t.Fatal("a token deleted the invoice")
	}
}
