package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// postMCP sends one raw JSON-RPC message to the agent endpoint. The cookie jar of
// the test env goes along only when cookies is true, so a test can show that a
// browser session alone does not get in.
func postMCP(t *testing.T, e *apiEnv, headers map[string]string, body string, cookies bool) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookies {
		for name, value := range e.jar {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode MCP answer %q: %v", raw, err)
		}
	}
	return resp.StatusCode, out
}

// callTool calls one tool and returns its text and whether the tool failed.
func callTool(t *testing.T, e *apiEnv, auth map[string]string, name string, args map[string]any) (string, bool) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, out := postMCP(t, e, auth, string(body), false)
	if status != http.StatusOK {
		t.Fatalf("tools/call %s: HTTP %d", name, status)
	}
	if errObj, ok := out["error"]; ok {
		t.Fatalf("tools/call %s: protocol error %v", name, errObj)
	}
	result := out["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	isError, _ := result["isError"].(bool)
	return text, isError
}

func mcpBearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestMCPNeedsAnAPITokenAndNotABrowserSession(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-auth@x.test")
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	if status, _ := postMCP(t, e, nil, ping, true); status != http.StatusUnauthorized {
		t.Fatalf("session cookie without a token = %d, want 401", status)
	}
	if status, _ := postMCP(t, e, map[string]string{"Authorization": "Bearer nope"}, ping, false); status != http.StatusUnauthorized {
		t.Fatalf("a bad token = %d, want 401", status)
	}
	token := apiTokenOf(t, e, "agent")
	if status, out := postMCP(t, e, mcpBearer(token), ping, false); status != http.StatusOK || out["result"] == nil {
		t.Fatalf("ping with a token = %d %v", status, out)
	}
}

func TestMCPInitializeAndToolList(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-list@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))

	status, out := postMCP(t, e, auth, `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, false)
	result, _ := out["result"].(map[string]any)
	if status != http.StatusOK || result["protocolVersion"] != "2025-06-18" || out["id"] != "init" {
		t.Fatalf("initialize = %d %v", status, out)
	}

	if status, _ := postMCP(t, e, auth, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, false); status != http.StatusAccepted {
		t.Fatalf("notification = %d, want 202", status)
	}

	_, out = postMCP(t, e, auth, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, false)
	tools := out["result"].(map[string]any)["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		byName[tool["name"].(string)] = tool
		if tool["inputSchema"] == nil {
			t.Fatalf("tool %v has no inputSchema", tool["name"])
		}
	}
	for _, name := range []string{"deal_move_stage", "calendar_event_create", "calendar_event_delete", "deals_list"} {
		if byName[name] == nil {
			t.Fatalf("tools/list has no %s", name)
		}
	}
	del := byName["calendar_event_delete"]["annotations"].(map[string]any)
	if del["destructiveHint"] != true || del["readOnlyHint"] != false {
		t.Fatalf("delete annotations = %v", del)
	}
	if byName["deals_list"]["annotations"].(map[string]any)["readOnlyHint"] != true {
		t.Fatal("deals_list is not marked read-only")
	}
}

func TestMCPBatchAndUnknownMethodAreProtocolErrors(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-errors@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))

	_, out := postMCP(t, e, auth, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, false)
	if code := out["error"].(map[string]any)["code"]; code != float64(-32600) {
		t.Fatalf("batch code = %v, want -32600", code)
	}
	_, out = postMCP(t, e, auth, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, false)
	if code := out["error"].(map[string]any)["code"]; code != float64(-32601) {
		t.Fatalf("unknown method code = %v, want -32601", code)
	}
	_, out = postMCP(t, e, auth, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope"}}`, false)
	if code := out["error"].(map[string]any)["code"]; code != float64(-32602) {
		t.Fatalf("unknown tool code = %v, want -32602", code)
	}
}

func TestMCPAgentRunsADealThroughItsTools(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-deal@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	clientID := insertTestClient(t, e, "Ромашка")

	text, failed := callTool(t, e, auth, "deal_create", map[string]any{"title": "Сайт", "client_id": clientID, "amount_cents": 250000})
	if failed {
		t.Fatalf("deal_create failed: %s", text)
	}
	var deal struct {
		ID       int64  `json:"id"`
		Stage    string `json:"stage"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal([]byte(text), &deal); err != nil {
		t.Fatalf("deal_create text is not the deal: %v", err)
	}
	if deal.Stage != "new" {
		t.Fatalf("new deal stage = %q", deal.Stage)
	}

	text, failed = callTool(t, e, auth, "deal_move_stage", map[string]any{"deal_id": deal.ID, "stage": "lost"})
	if !failed || !strings.Contains(text, "lost_reason") {
		t.Fatalf("lost without a reason = %v %q, want a failed call naming lost_reason", failed, text)
	}
	if _, failed = callTool(t, e, auth, "deal_move_stage", map[string]any{"deal_id": deal.ID, "stage": "lost", "lost_reason": "бюджет"}); failed {
		t.Fatal("lost with a reason failed")
	}

	text, failed = callTool(t, e, auth, "deals_list", map[string]any{"stage": "lost"})
	if failed || !strings.Contains(text, `"stage":"lost"`) {
		t.Fatalf("deals_list = %v %q", failed, text)
	}

	if _, failed = callTool(t, e, auth, "deal_create", map[string]any{"title": "Ошибка", "bogus": true}); !failed {
		t.Fatal("a misspelt argument was accepted")
	}
}

func TestMCPReadOnlyTokenReadsButWritesNothing(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-readonly@x.test")
	clientID := insertTestClient(t, e, "Лютик")
	page := readBody(t, e.do("POST", "/api/tokens", url.Values{"name": {"reader"}, "read_only": {"1"}}, nil))
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	auth := mcpBearer(mustUnescape(t, raw))

	if _, failed := callTool(t, e, auth, "clients_list", map[string]any{}); failed {
		t.Fatal("a read-only token could not read clients")
	}
	text, failed := callTool(t, e, auth, "deal_create", map[string]any{"title": "Нельзя", "client_id": clientID})
	if !failed || !strings.Contains(text, "только для чтения") {
		t.Fatalf("write by a read-only token = %v %q", failed, text)
	}
	var count int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM crm_deals WHERE title = 'Нельзя'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a read-only token wrote %d deal(s)", count)
	}
}

func TestMCPWriteLeavesAnAuditTrail(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-audit@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	if _, failed := callTool(t, e, auth, "calendar_event_create", map[string]any{
		"title": "Созвон", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339),
	}); failed {
		t.Fatal("calendar_event_create failed")
	}
	var count int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE action = 'mcp.write' AND target = 'calendar_event_create' AND meta = 'агентом через MCP'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit rows for the agent write = %d, want 1", count)
	}
}

// insertTestClient adds a client to the workspace the way the reads tests do.
func insertTestClient(t *testing.T, e *apiEnv, name string) int64 {
	t.Helper()
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000")
	var id int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at) VALUES (?, ?, '', '', 'RUB', '', 0, ?, ?) RETURNING id`,
		teamID, name, now, now).Scan(&id); err != nil {
		t.Fatalf("client: %v", err)
	}
	return id
}

// A switched-off section takes its tools away: they leave the list and a call to
// them is refused, as the section's screens are.
func TestMCPHidesAndRefusesToolsOfASwitchedOffSection(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-section@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))

	resp := e.do("POST", "/api/team/modules", url.Values{"preset": {"solo"}}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("switch to the solo preset: status %d", resp.StatusCode)
	}

	_, out := postMCP(t, e, auth, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, false)
	for _, raw := range out["result"].(map[string]any)["tools"].([]any) {
		name := raw.(map[string]any)["name"]
		if name == "deal_create" || name == "calendar_events_list" {
			t.Fatalf("tools/list still offers %v with its section off", name)
		}
	}
	if _, failed := callTool(t, e, auth, "deals_list", map[string]any{}); !failed {
		t.Fatal("a call to a tool of a switched-off section was not refused")
	}
}

// A member's token reaches the same tools as an owner's, and the workflows refuse
// what a member may not do: changing a client, and making a deal for a colleague.
// The input check still names the field.
func TestMCPMemberIsRefusedWhatItMayNotDo(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("mcp-member-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("mcp-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"mcp-member@x.test": member}, "mcp-member@x.test")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))
	var studioID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams WHERE name = 'Студия' ORDER BY id DESC LIMIT 1`).Scan(&studioID); err != nil {
		t.Fatalf("load the shared workspace: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000")
	var clientID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at) VALUES (?, 'Ромашка', '', '', 'RUB', '', 0, ?, ?) RETURNING id`,
		studioID, now, now).Scan(&clientID); err != nil {
		t.Fatalf("client: %v", err)
	}

	text, failed := callTool(t, member, memberAuth, "client_update", map[string]any{"client_id": clientID, "name": "Другое"})
	if !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member changed a client: failed=%v text=%q", failed, text)
	}
	var name string
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT name FROM clients WHERE id = ?`, clientID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Ромашка" {
		t.Fatalf("the refused client change was written: name = %q", name)
	}

	if text, failed = callTool(t, member, memberAuth, "client_create", map[string]any{"name": "Новый"}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member created a client: failed=%v text=%q", failed, text)
	}

	var ownerID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT user_id FROM memberships WHERE team_id = ? AND role = 'owner' LIMIT 1`, studioID).Scan(&ownerID); err != nil {
		t.Fatalf("load the owner: %v", err)
	}
	if text, failed = callTool(t, member, memberAuth, "deal_create", map[string]any{"title": "Для коллеги", "owner_id": ownerID}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member made a deal for a colleague: failed=%v text=%q", failed, text)
	}

	if text, failed = callTool(t, member, memberAuth, "deal_create", map[string]any{"title": "Сделка", "close_date": "2026-13-01"}); !failed || !strings.Contains(text, "close date") {
		t.Fatalf("an impossible close date: failed=%v text=%q, want the field named", failed, text)
	}
}

// invoice_get answers with the lines the invoice page shows: a by-person line loses the
// member's name while the workspace hides the names, and keeps it when it does not.
func TestMCPInvoiceHidesExecutorNamesLikeThePage(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-invoice@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	var ownerID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-MCP-1", Client: "Acme", Start: start, End: start.Add(2 * time.Hour),
		Lines: []model.InvoiceLine{{Label: "Acme · consulting · U", Seconds: 7200, RateCents: 10000, AmountCents: 20000}},
	})
	if err != nil {
		t.Fatalf("create the invoice: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE invoices SET by_person = 1 WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	call := func() string {
		text, failed := callTool(t, e, auth, "invoice_get", map[string]any{"invoice_id": inv.ID})
		if failed {
			t.Fatalf("invoice_get failed: %s", text)
		}
		return text
	}

	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE teams SET hide_executor_names = 1 WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	if text := call(); strings.Contains(text, "consulting · U") || !strings.Contains(text, `"Acme · consulting"`) {
		t.Fatalf("hidden names still on the lines: %s", text)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE teams SET hide_executor_names = 0 WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	if text := call(); !strings.Contains(text, "consulting · U") {
		t.Fatalf("names hidden although the switch is off: %s", text)
	}
}

// seedInvoiceForMCP issues a draft invoice of one line through the workflow the page uses, and
// returns its id and the workspace it belongs to.
func seedInvoiceForMCP(t *testing.T, e *apiEnv, number string) (int64, int64) {
	t.Helper()
	var ownerID, teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT user_id, team_id FROM memberships WHERE role = 'owner' ORDER BY team_id DESC LIMIT 1`).Scan(&ownerID, &teamID); err != nil {
		t.Fatalf("find the owner: %v", err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: number, Client: "Acme", Start: start, End: start.Add(2 * time.Hour),
		Lines: []model.InvoiceLine{{Label: "Acme · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000}},
	})
	if err != nil {
		t.Fatalf("create the invoice: %v", err)
	}
	return inv.ID, teamID
}

// The owner's token lists the invoices and the pay runs of the workspace, with totals in cents.
func TestMCPManagerListsInvoicesAndPayrollRuns(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-lists@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	invoiceID, teamID := seedInvoiceForMCP(t, e, "INV-MCP-LIST")

	text, failed := callTool(t, e, auth, "invoices_list", map[string]any{})
	if failed || !strings.Contains(text, `"number":"INV-MCP-LIST"`) || !strings.Contains(text, `"total_cents":20000`) {
		t.Fatalf("invoices_list = %v %q, want the invoice with its total in cents", failed, text)
	}
	if !strings.Contains(text, `"id":`+strconv.FormatInt(invoiceID, 10)) {
		t.Fatalf("invoices_list does not carry the invoice id %d: %q", invoiceID, text)
	}

	if _, err := e.db.TestSQL().ExecContext(t.Context(),
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, created_at)
		 VALUES (?, 'PAY-MCP-LIST', '2026-10-01T00:00:00Z', '2026-10-08T00:00:00Z', 'paid', '2026-10-08T09:00:00Z')`, teamID); err != nil {
		t.Fatalf("seed the pay run: %v", err)
	}
	text, failed = callTool(t, e, auth, "payroll_runs_list", map[string]any{})
	if failed || !strings.Contains(text, `"number":"PAY-MCP-LIST"`) || !strings.Contains(text, `"status":"paid"`) {
		t.Fatalf("payroll_runs_list = %v %q, want the pay run", failed, text)
	}
}

// deal_stages_list shows the five defaults of a new workspace; a member's token reads
// them but cannot add a stage.
func TestMCPStagesListsTheDefaultsAndRefusesAMemberStageCreate(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("mcp-stages-owner@x.test")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	text, failed := callTool(t, owner, auth, "deal_stages_list", map[string]any{})
	if failed {
		t.Fatalf("deal_stages_list failed: %s", text)
	}
	var out struct {
		Stages []struct {
			Key      string `json:"key"`
			Position int    `json:"position"`
			Kind     string `json:"kind"`
		} `json:"stages"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("deal_stages_list text is not the stages: %v", err)
	}
	if len(out.Stages) != 5 || out.Stages[0].Key != "new" || out.Stages[3].Kind != "won" || out.Stages[4].Position != 5 {
		t.Fatalf("stages = %+v, want the five defaults", out.Stages)
	}

	member := newAPIEnvSharedDB(t, owner)
	member.register("mcp-stages-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"mcp-stages-member@x.test": member}, "mcp-stages-member@x.test")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))
	if text, failed = callTool(t, member, memberAuth, "deal_stages_list", map[string]any{}); failed {
		t.Fatalf("a member cannot read the stages: %s", text)
	}
	if text, failed = callTool(t, member, memberAuth, "deal_stage_create", map[string]any{"name": "Чужой"}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member added a stage: failed=%v text=%q", failed, text)
	}
}
