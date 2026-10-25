package web

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// studioOwnerAndMember makes a shared workspace with one member and returns both
// sessions. The tests mint their tokens after the workspace exists, so the tokens
// resolve to it.
func studioOwnerAndMember(t *testing.T, tag string) (*apiEnv, *apiEnv) {
	t.Helper()
	owner := newAPIEnv(t)
	owner.register("owner-" + tag + "@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("member-" + tag + "@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"member-" + tag + "@x.test": member}, "member-"+tag+"@x.test")
	return owner, member
}

func TestMCPMoreToolsAreListedWithTheirHints(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "list")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))

	_, out := postMCP(t, owner, auth, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, false)
	byName := map[string]map[string]any{}
	for _, raw := range out["result"].(map[string]any)["tools"].([]any) {
		tool := raw.(map[string]any)
		byName[tool["name"].(string)] = tool
	}
	for _, name := range []string{
		"goals_list", "goal_set", "goal_delete", "tags_list", "tag_create", "members_list",
		"schedule_entries_list", "schedule_cell_set", "invoices_list", "invoice_get",
		"payroll_runs_list", "timesheet_approvals_list",
	} {
		if byName[name] == nil {
			t.Fatalf("tools/list has no %s", name)
		}
	}
	hint := func(name, key string) any { return byName[name]["annotations"].(map[string]any)[key] }
	if hint("goals_list", "readOnlyHint") != true || hint("tag_create", "readOnlyHint") != false {
		t.Fatal("goals_list or tag_create carries the wrong read-only hint")
	}
	if hint("goal_delete", "destructiveHint") != true {
		t.Fatal("goal_delete is not marked destructive")
	}
}

// A member's token is refused every manager-only tool, and the refusal writes nothing.
func TestMCPMemberIsRefusedTheManagerOnlyTools(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "refused")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	refused := []struct {
		name string
		args map[string]any
	}{
		{"members_list", map[string]any{}},
		{"invoices_list", map[string]any{}},
		{"invoice_get", map[string]any{"invoice_id": 1}},
		{"payroll_runs_list", map[string]any{}},
		{"goal_set", map[string]any{"activity": "Фокус", "period": "daily", "minutes": 60}},
		{"goal_delete", map[string]any{"activity": "Фокус", "period": "daily"}},
	}
	for _, tc := range refused {
		text, failed := callTool(t, member, memberAuth, tc.name, tc.args)
		if !failed || !strings.Contains(text, "прав") {
			t.Fatalf("a member called %s: failed=%v text=%q, want the forbidden text", tc.name, failed, text)
		}
	}

	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	if text, failed := callTool(t, owner, ownerAuth, "goals_list", map[string]any{}); failed || strings.Contains(text, "Фокус") {
		t.Fatalf("a refused goal_set was written: failed=%v text=%q", failed, text)
	}
}

func TestMCPOwnerSetsAGoalThatEveryoneReads(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "goals")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	if text, failed := callTool(t, owner, ownerAuth, "goal_set", map[string]any{"activity": "Фокус", "period": "daily", "minutes": 120}); failed {
		t.Fatalf("goal_set: %s", text)
	}
	text, failed := callTool(t, member, memberAuth, "goals_list", map[string]any{})
	if failed || !strings.Contains(text, `"activity_name":"Фокус"`) || !strings.Contains(text, `"target_minutes":120`) {
		t.Fatalf("a member does not see the goal: failed=%v text=%q", failed, text)
	}

	if text, failed := callTool(t, owner, ownerAuth, "goal_set", map[string]any{"activity": "Фокус", "period": "yearly", "minutes": 60}); !failed || !strings.Contains(text, "period") {
		t.Fatalf("a bad period: failed=%v text=%q, want the field named", failed, text)
	}
	if text, failed := callTool(t, member, memberAuth, "goals_list", map[string]any{"period": "yearly"}); !failed || !strings.Contains(text, "period") {
		t.Fatalf("a bad goals_list period: failed=%v text=%q, want the field named", failed, text)
	}

	if _, failed := callTool(t, owner, ownerAuth, "goal_delete", map[string]any{"activity": "Фокус", "period": "daily"}); failed {
		t.Fatal("goal_delete of the goal failed")
	}
	if text, failed := callTool(t, member, memberAuth, "goals_list", map[string]any{}); failed || strings.Contains(text, "Фокус") {
		t.Fatalf("the deleted goal is still listed: failed=%v text=%q", failed, text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "goal_delete", map[string]any{"activity": "Фокус", "period": "daily"}); !failed {
		t.Fatalf("a second goal_delete succeeded: %s", text)
	}
}

func TestMCPTagCreateThenTagsList(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "tags")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	if text, failed := callTool(t, owner, ownerAuth, "tag_create", map[string]any{"name": "Срочно"}); failed || !strings.Contains(text, `"name":"срочно"`) {
		t.Fatalf("tag_create by the owner: failed=%v text=%q, want the lower-case name", failed, text)
	}
	if text, failed := callTool(t, member, memberAuth, "tag_create", map[string]any{"name": "ревью"}); failed {
		t.Fatalf("tag_create by a member: %s", text)
	}
	text, failed := callTool(t, owner, ownerAuth, "tags_list", map[string]any{})
	if failed || !strings.Contains(text, "срочно") || !strings.Contains(text, "ревью") {
		t.Fatalf("tags_list = %v %q", failed, text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "tag_create", map[string]any{"name": "  "}); !failed || !strings.Contains(text, "name") {
		t.Fatalf("a blank tag: failed=%v text=%q, want the field named", failed, text)
	}
}

// A read-only token reads goals and tags and writes neither.
func TestMCPReadOnlyTokenReadsGoalsAndTagsButWritesNone(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "readonly")
	page := readBody(t, owner.do("POST", "/api/tokens", url.Values{"name": {"reader"}, "read_only": {"1"}}, nil))
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	auth := mcpBearer(mustUnescape(t, raw))

	for _, name := range []string{"goals_list", "tags_list"} {
		if _, failed := callTool(t, owner, auth, name, map[string]any{}); failed {
			t.Fatalf("a read-only token could not call %s", name)
		}
	}
	text, failed := callTool(t, owner, auth, "tag_create", map[string]any{"name": "нельзя"})
	if !failed || !strings.Contains(text, "только для чтения") {
		t.Fatalf("tag_create by a read-only token = %v %q", failed, text)
	}
	if text, failed = callTool(t, owner, auth, "goal_set", map[string]any{"activity": "Нельзя", "period": "daily", "minutes": 30}); !failed || !strings.Contains(text, "только для чтения") {
		t.Fatalf("goal_set by a read-only token = %v %q", failed, text)
	}
	var count int
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM tags WHERE name = 'нельзя'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a read-only token wrote %d tag(s)", count)
	}
}

// The plan cell is set by a manager. A member who leads no project is refused it,
// and both can read the week.
func TestMCPPlanCellIsSetByAManagerAndReadBack(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "plan")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	resp := owner.do("POST", "/projects/new", url.Values{"name": {"Сайт"}, "rate": {"3000"}}, nil)
	resp.Body.Close()
	var projectID, ownerID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Сайт'`).Scan(&projectID); err != nil {
		t.Fatalf("load the project: %v", err)
	}
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'owner-plan@x.test'`).Scan(&ownerID); err != nil {
		t.Fatalf("load the owner: %v", err)
	}

	if text, failed := callTool(t, owner, ownerAuth, "schedule_cell_set", map[string]any{
		"project_id": projectID, "user_id": ownerID, "date": "2026-10-26", "minutes": 240,
	}); failed {
		t.Fatalf("schedule_cell_set by the owner: %s", text)
	}
	if text, failed := callTool(t, member, memberAuth, "schedule_cell_set", map[string]any{
		"project_id": projectID, "user_id": ownerID, "date": "2026-10-27", "minutes": 60,
	}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member who leads nothing set a cell: failed=%v text=%q", failed, text)
	}

	// 2026-10-28 is a Wednesday: the read returns the week that starts on the Monday before it.
	text, failed := callTool(t, member, memberAuth, "schedule_entries_list", map[string]any{"from": "2026-10-28"})
	if failed {
		t.Fatalf("schedule_entries_list: %s", text)
	}
	var plan struct {
		Days   []string `json:"days"`
		People []struct {
			UserID  int64 `json:"user_id"`
			Planned int   `json:"planned_minutes"`
			Lines   []struct {
				ProjectName string `json:"project_name"`
				Minutes     [7]int `json:"minutes"`
			} `json:"lines"`
		} `json:"people"`
	}
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("schedule_entries_list is not the plan: %v", err)
	}
	if len(plan.Days) != 7 || plan.Days[0] != "2026-10-26" {
		t.Fatalf("days = %v, want the week of 2026-10-26", plan.Days)
	}
	found := false
	for _, person := range plan.People {
		if person.UserID != ownerID {
			continue
		}
		found = person.Planned == 240 && len(person.Lines) == 1 &&
			person.Lines[0].ProjectName == "Сайт" && person.Lines[0].Minutes[0] == 240
	}
	if !found {
		t.Fatalf("the owner's 240 minutes are not in the plan: %s", text)
	}
}

// Invoices, payroll runs and approved weeks read empty in a new workspace, and a
// member cannot reach the money reads. The approvals are open to every member.
func TestMCPMoneyReadsAreEmptyAndManagerOnly(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "money")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	if text, failed := callTool(t, owner, ownerAuth, "invoices_list", map[string]any{}); failed || !strings.Contains(text, `"invoices":[]`) {
		t.Fatalf("invoices_list = %v %q", failed, text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "payroll_runs_list", map[string]any{}); failed || !strings.Contains(text, `"runs":[]`) {
		t.Fatalf("payroll_runs_list = %v %q", failed, text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "invoice_get", map[string]any{"invoice_id": 999}); !failed {
		t.Fatalf("invoice_get of a missing invoice succeeded: %s", text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "members_list", map[string]any{}); failed || !strings.Contains(text, `"role":"owner"`) || !strings.Contains(text, `"role":"member"`) {
		t.Fatalf("members_list = %v %q, want the owner and the member", failed, text)
	}

	if text, failed := callTool(t, member, memberAuth, "timesheet_approvals_list", map[string]any{"from": "2026-10-01", "to": "2026-10-31"}); failed || !strings.Contains(text, `"approvals":[]`) {
		t.Fatalf("a member could not read the approved weeks: %v %q", failed, text)
	}
	if text, failed := callTool(t, member, memberAuth, "timesheet_approvals_list", map[string]any{"from": "2026-10-31", "to": "2026-10-01"}); !failed || !strings.Contains(text, "to") {
		t.Fatalf("a window that ends before it starts: failed=%v text=%q", failed, text)
	}
}
