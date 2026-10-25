package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// The view counts a lead's first response against the two-hour target: ninety minutes is within
// it, three hours is late, and a lead with no activity reports the age it has reached (CRM-16).
func TestLeadViewCountsTheFirstResponseAgainstTheTarget(t *testing.T) {
	created := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	lead := model.CRMLead{ID: 1, CreatedAt: created}
	now := created.Add(45 * time.Minute)

	if v := crmLeadViewOf(lead, now); v.WaitingMinutes != 45 || v.ResponseMinutes != 0 || v.WithinTarget {
		t.Fatalf("waiting view = %+v, want 45 minutes of waiting and no response", v)
	}

	lead.FirstResponseAt = created.Add(90 * time.Minute)
	if v := crmLeadViewOf(lead, now); v.ResponseMinutes != 90 || !v.WithinTarget || v.WaitingMinutes != 0 {
		t.Fatalf("answered after 90 minutes = %+v, want within the target", v)
	}

	lead.FirstResponseAt = created.Add(3 * time.Hour)
	if v := crmLeadViewOf(lead, now); v.ResponseMinutes != 180 || v.WithinTarget {
		t.Fatalf("answered after 3 hours = %+v, want late", v)
	}
}

// A member reads the response numbers of the workspace over HTTP and through the lead tool, and
// a period that is missing or empty is refused with the field that is wrong (CRM-16).
func TestLeadResponseStatsSeenByAMember(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("lead-stats-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("lead-stats-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"lead-stats-member@x.test": member}, "lead-stats-member@x.test")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	text, failed := callTool(t, member, memberAuth, "lead_create", map[string]any{"company": "Лотос", "contact_name": "Анна"})
	if failed {
		t.Fatalf("a member records a lead: %s", text)
	}
	var lead struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &lead); err != nil || lead.ID == 0 {
		t.Fatalf("lead_create text = %q, %v", text, err)
	}
	// Answered at once, so the lead is within the target with a response of zero minutes.
	if text, failed = callTool(t, member, memberAuth, "lead_activity_add", map[string]any{"lead_id": lead.ID, "kind": "call", "body": "Позвонил"}); failed {
		t.Fatalf("a member logs the first activity: %s", text)
	}
	if text, failed = callTool(t, member, memberAuth, "lead_create", map[string]any{"company": "Ромашка", "email": "buy@romashka.test"}); failed {
		t.Fatalf("a second lead: %s", text)
	}

	list := member.do("GET", "/api/crm/leads", nil, nil)
	var leads crmLeadsResponse
	decodeBody(t, list, &leads)
	answered, waiting := leads.Leads[1], leads.Leads[0]
	if answered.ID != lead.ID || !answered.WithinTarget || answered.ResponseMinutes != 0 || answered.WaitingMinutes != 0 {
		t.Fatalf("answered lead in the list = %+v, want within the target", answered)
	}
	if waiting.WithinTarget || waiting.ResponseMinutes != 0 {
		t.Fatalf("waiting lead in the list = %+v, want no response", waiting)
	}

	resp := member.do("GET", "/api/crm/leads/response-stats?from=2000-01-01&to=2100-01-01", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a member reads the stats: status %d", resp.StatusCode)
	}
	var stats crmLeadResponseStatsView
	decodeBody(t, resp, &stats)
	if stats.Created != 2 || stats.Answered != 1 || stats.WithinTarget != 1 || stats.TargetMinutes != model.LeadResponseTargetMinutes {
		t.Fatalf("stats = %+v, want 2 created, 1 answered, 1 within the target", stats)
	}
	if stats.MedianMinutes == nil || *stats.MedianMinutes != 0 {
		t.Fatalf("median = %v, want 0 minutes", stats.MedianMinutes)
	}

	text, failed = callTool(t, member, memberAuth, "lead_response_stats", map[string]any{"from": "2000-01-01", "to": "2100-01-01"})
	if failed {
		t.Fatalf("lead_response_stats: %s", text)
	}
	var viaTool crmLeadResponseStatsView
	if err := json.Unmarshal([]byte(text), &viaTool); err != nil || viaTool.Created != 2 || viaTool.WithinTarget != 1 {
		t.Fatalf("lead_response_stats = %q, %v", text, err)
	}

	assertFieldError(t, member.do("GET", "/api/crm/leads/response-stats", nil, nil), http.StatusBadRequest, "from")
	assertFieldError(t, member.do("GET", "/api/crm/leads/response-stats?from=2026-10-25&to=2026-10-01", nil, nil), http.StatusBadRequest, "to")
	if text, failed = callTool(t, member, memberAuth, "lead_response_stats", map[string]any{"from": "2026-10-25"}); !failed || !strings.Contains(text, "to") {
		t.Fatalf("lead_response_stats without to: failed=%v text=%q, want the field named", failed, text)
	}
}
