package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A member records a lead and logs on it, but cannot convert any lead: a conversion creates a
// client, and only a manager creates clients. The refusal reads as the forbidden text.
func TestMCPLeadCreateForAMemberAndConvertOnlyTheOwnLead(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("mcp-lead-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("mcp-lead-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"mcp-lead-member@x.test": member}, "mcp-lead-member@x.test")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "owner"))

	text, failed := callTool(t, member, memberAuth, "lead_create", map[string]any{"company": "Лотос", "contact_name": "Анна", "source": "referral"})
	if failed {
		t.Fatalf("a member records a lead: %s", text)
	}
	var mine struct {
		ID      int64  `json:"id"`
		OwnerID int64  `json:"owner_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &mine); err != nil || mine.ID == 0 || mine.Status != "new" {
		t.Fatalf("lead_create text = %q, %v", text, err)
	}

	text, failed = callTool(t, owner, ownerAuth, "lead_create", map[string]any{"company": "Ромашка", "email": "buy@romashka.test"})
	if failed {
		t.Fatalf("the owner records a lead: %s", text)
	}
	var theirs struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &theirs); err != nil || theirs.ID == 0 {
		t.Fatalf("owner lead text = %q, %v", text, err)
	}

	if text, failed = callTool(t, member, memberAuth, "lead_convert", map[string]any{"lead_id": theirs.ID}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member converted a lead it does not own: failed=%v text=%q", failed, text)
	}
	if text, failed = callTool(t, member, memberAuth, "lead_status_set", map[string]any{"lead_id": theirs.ID, "status": "contacted"}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member moved a lead it does not own: failed=%v text=%q", failed, text)
	}
	if text, failed = callTool(t, member, memberAuth, "lead_update", map[string]any{"lead_id": theirs.ID, "phone": "+7 900 111-22-33"}); failed {
		t.Fatalf("a member edits a lead's details: %s", text)
	}

	if text, failed = callTool(t, member, memberAuth, "lead_convert", map[string]any{"lead_id": mine.ID}); !failed || !strings.Contains(text, "прав") {
		t.Fatalf("a member converted its own lead: failed=%v text=%q", failed, text)
	}

	if text, failed = callTool(t, member, memberAuth, "lead_activity_add", map[string]any{"lead_id": theirs.ID, "kind": "call", "body": "Позвонил"}); failed {
		t.Fatalf("a member logs on a lead: %s", text)
	}
	text, failed = callTool(t, member, memberAuth, "activities_list", map[string]any{"lead_id": theirs.ID})
	if failed || !strings.Contains(text, "Позвонил") {
		t.Fatalf("the lead's timeline = %v %q", failed, text)
	}

	if text, failed = callTool(t, owner, ownerAuth, "lead_convert", map[string]any{"lead_id": mine.ID}); failed {
		t.Fatalf("the manager converts the lead: %s", text)
	}

	text, failed = callTool(t, member, memberAuth, "lead_list", map[string]any{"status": "converted"})
	if failed || !strings.Contains(text, "Лотос") {
		t.Fatalf("lead_list converted = %v %q", failed, text)
	}
	if text, failed = callTool(t, member, memberAuth, "lead_list", map[string]any{"status": "won"}); !failed {
		t.Fatalf("an unknown status filter was accepted: %q", text)
	}
	if text, failed = callTool(t, member, memberAuth, "lead_create", map[string]any{"source": "website"}); !failed || !strings.Contains(text, "contact") {
		t.Fatalf("a lead without a contact: failed=%v text=%q, want the field named", failed, text)
	}
}

// The agent lists the lead tools in the CRM section, so they are hidden with the section.
func TestMCPLeadToolsAreListedInTheCRMSection(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-lead-list@x.test")
	status, out := postMCP(t, e, mcpBearer(apiTokenOf(t, e, "agent")), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, false)
	if status != http.StatusOK {
		t.Fatalf("tools/list = %d", status)
	}
	raw, _ := json.Marshal(out)
	for _, name := range []string{"lead_list", "lead_create", "lead_update", "lead_status_set", "lead_convert", "lead_activity_add"} {
		if !strings.Contains(string(raw), `"`+name+`"`) {
			t.Fatalf("tool %s is not listed", name)
		}
	}
}
