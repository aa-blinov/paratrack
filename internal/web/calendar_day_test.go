package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// An all-day event is sent as its first and last day and comes back as midnight of the first
// day and midnight after the last. A push to a provider is refused for it by name.
func TestAllDayEventIsSentAsWholeDays(t *testing.T) {
	e := newAPIEnv(t)
	e.register("all-day-web@x.test")

	created := e.postJSON("/api/calendar/events", map[string]any{
		"title": "Выставка", "starts_at": "2026-10-30", "ends_at": "2026-11-01", "all_day": true, "shared": true,
	})
	status := created.StatusCode
	var event struct {
		ID       int64  `json:"id"`
		AllDay   bool   `json:"all_day"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
	}
	decodeBody(t, created, &event)
	if status != http.StatusOK || !event.AllDay || !strings.HasPrefix(event.StartsAt, "2026-10-30T00:00:00") || !strings.HasPrefix(event.EndsAt, "2026-11-02T00:00:00") {
		t.Fatalf("all-day event = status %d, %+v; want whole days 30 Oct to 1 Nov", status, event)
	}

	assertFieldError(t, e.postJSON("/api/calendar/events", map[string]any{
		"title": "Не в провайдер", "starts_at": "2026-10-30", "ends_at": "2026-10-30", "all_day": true, "sync_push": true,
	}), http.StatusBadRequest, "sync_push")
}

// A contact is changed by a manager, or by the member who created it. The member who did not
// create it is refused with the forbidden text, and a name that is left blank is refused by name.
func TestContactEditRouteFollowsTheCreatorRule(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("contact-edit-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("contact-edit-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"contact-edit-member@x.test": member}, "contact-edit-member@x.test")

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
	ownerPath := fmt.Sprintf("/api/crm/contacts/%d", byOwner.ID)
	refused := member.postJSON(ownerPath, map[string]any{"name": "Чужая"})
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("a member edits the manager's contact: status %d, want 403", refused.StatusCode)
	}

	var own struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, member.postJSON("/api/crm/contacts", map[string]any{"client_id": clientID, "name": "Коллега"}), &own)
	ownPath := fmt.Sprintf("/api/crm/contacts/%d", own.ID)
	edited := member.postJSON(ownPath, map[string]any{"role": "Бухгалтер"})
	status := edited.StatusCode
	var view struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		CanEdit bool   `json:"can_edit"`
	}
	decodeBody(t, edited, &view)
	if status != http.StatusOK || view.Name != "Коллега" || view.Role != "Бухгалтер" || !view.CanEdit {
		t.Fatalf("a member edits the contact they created: status %d, %+v", status, view)
	}

	assertFieldError(t, member.postJSON(ownPath, map[string]any{"name": " "}), http.StatusBadRequest, "contact name")
}
