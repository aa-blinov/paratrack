package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The calendar screen opens for a member who has connected no calendar. The provider list
// it reads is an empty list, not null, because the event form reads its length.
func TestCalendarPageSendsAnEmptyProviderList(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-empty-providers@x.test")
	resp := e.do("GET", "/calendar", nil, nil)
	status := resp.StatusCode
	body := readBody(t, resp)
	if status != http.StatusOK || !strings.Contains(body, `"SyncProviders":[]`) {
		t.Fatalf("calendar page: status %d, the provider list is not an empty list", status)
	}
}

// A contact is deleted by a manager, or by the member who created it. The list tells each
// caller which contacts they may delete.
func TestContactDeleteRouteFollowsTheCreatorRule(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("contact-route-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("contact-route-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"contact-route-member@x.test": member}, "contact-route-member@x.test")

	created := owner.do("POST", "/clients", url.Values{"name": {"Ромашка"}}, nil)
	created.Body.Close()
	var clientID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM clients WHERE name = 'Ромашка'`).Scan(&clientID); err != nil {
		t.Fatal(err)
	}

	var byOwner struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, owner.postJSON("/api/crm/contacts", map[string]any{"client_id": clientID, "name": "Анна"}), &byOwner)
	if byOwner.ID == 0 {
		t.Fatal("the manager adds a contact: no id in the answer")
	}
	ownerPath := fmt.Sprintf("/api/crm/contacts/%d/delete", byOwner.ID)

	refused := member.do("POST", ownerPath, nil, nil)
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("a member deletes the manager's contact: status %d, want 403", refused.StatusCode)
	}

	var list struct {
		Contacts []struct {
			ID        int64 `json:"id"`
			CanDelete bool  `json:"can_delete"`
		} `json:"contacts"`
	}
	decodeBody(t, member.do("GET", fmt.Sprintf("/api/crm/contacts?client_id=%d", clientID), nil, nil), &list)
	if len(list.Contacts) != 1 || list.Contacts[0].CanDelete {
		t.Fatalf("member's list = %+v, want the manager's contact without can_delete", list.Contacts)
	}

	var own struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, member.postJSON("/api/crm/contacts", map[string]any{"client_id": clientID, "name": "Коллега"}), &own)
	removed := member.do("POST", fmt.Sprintf("/api/crm/contacts/%d/delete", own.ID), nil, nil)
	removed.Body.Close()
	if removed.StatusCode != http.StatusNoContent {
		t.Fatalf("a member deletes the contact they created: status %d, want 204", removed.StatusCode)
	}

	gone := owner.do("POST", ownerPath, nil, nil)
	gone.Body.Close()
	if gone.StatusCode != http.StatusNoContent {
		t.Fatalf("the manager deletes a contact: status %d, want 204", gone.StatusCode)
	}
}

// A reversed period and a client id of zero are refused with a readable text, not with the
// internal error text the agent cannot act on.
func TestMCPRefusesBadRangeAndClientIDWithoutInternalError(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-refusals@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))

	text, failed := callTool(t, e, auth, "time_sessions_list", map[string]any{"from": "2026-10-30", "to": "2026-10-01"})
	if !failed || text == "internal server error" {
		t.Fatalf("reversed period = %q (failed %v), want a field refusal", text, failed)
	}
	text, failed = callTool(t, e, auth, "client_update", map[string]any{"client_id": 0, "name": "Х"})
	if !failed || text == "internal server error" {
		t.Fatalf("client id zero = %q (failed %v), want a not-found refusal", text, failed)
	}
}
