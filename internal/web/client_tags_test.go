package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// forbiddenTexts are the refusal a member gets, in both languages a token or a page may use.
func forbiddenTexts(t *testing.T) []string {
	t.Helper()
	return []string{i18n.T(i18n.Ru, "err.forbiddenAction"), i18n.T(i18n.En, "err.forbiddenAction")}
}

// A manager sets a client's tags over the API, and a member is refused with the forbidden
// text. The tags reach the card, and the CRM search finds the client by its tag and its
// contact by phone, with spaces and dashes in the query. The billing email is for managers.
func TestClientTagsOverHTTPAndTheCRMSearch(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "client-tags-http")
	clientID := insertClientInTeam(t, owner, mcpStudioID(t, owner), "Ромашка")
	if _, err := owner.db.TestSQL().ExecContext(t.Context(), `UPDATE clients SET email = ? WHERE id = ?`, "buh@romashka.test", clientID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/clients/%d/tags", clientID)

	refused := member.postJSON(path, map[string]any{"tags": []string{"vip"}})
	var refusal apiErrorResponse
	decodeBody(t, refused, &refusal)
	if refused.StatusCode != http.StatusForbidden || !slices.Contains(forbiddenTexts(t), refusal.Error) {
		t.Fatalf("member sets tags: status %d, error %q; want 403 with the forbidden text", refused.StatusCode, refusal.Error)
	}
	assertFieldError(t, owner.postJSON(path, map[string]any{}), http.StatusBadRequest, "tags")
	tooMany := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11"}
	assertFieldError(t, owner.postJSON(path, map[string]any{"tags": tooMany}), http.StatusBadRequest, "tags")

	saved := owner.postJSON(path, map[string]any{"tags": []string{"VIP", " Постоянный ", "vip"}})
	var stored clientTagsResponse
	decodeBody(t, saved, &stored)
	if saved.StatusCode != http.StatusOK || !slices.Equal(stored.Tags, []string{"vip", "постоянный"}) {
		t.Fatalf("manager saves tags: status %d, stored %v", saved.StatusCode, stored.Tags)
	}
	if card := readBody(t, member.do("GET", fmt.Sprintf("/clients/%d", clientID), nil, nil)); !strings.Contains(card, "постоянный") {
		t.Fatal("the member's card does not show the client's tag")
	}

	if contact := member.postJSON("/api/crm/contacts", map[string]any{
		"client_id": clientID, "name": "Анна Смирнова", "role": "Директор",
		"email": "anna@romashka.test", "phone": "+7 (916) 123-45-67",
	}); contact.StatusCode != http.StatusOK {
		t.Fatalf("add contact: status %d", contact.StatusCode)
	}

	search := func(e *apiEnv, query url.Values) searchResponse {
		t.Helper()
		var out searchResponse
		decodeBody(t, e.do("GET", "/api/search?"+query.Encode(), nil, map[string]string{"Accept": "application/json"}), &out)
		return out
	}
	groupOf := func(out searchResponse, kind string) *searchGroup {
		for i := range out.Groups {
			if out.Groups[i].Kind == kind {
				return &out.Groups[i]
			}
		}
		return nil
	}

	byPhone := groupOf(search(member, url.Values{"q": {"916 123-45"}, "limit": {"50"}}), "contacts")
	if byPhone == nil || len(byPhone.Items) != 1 || byPhone.Items[0].Href != fmt.Sprintf("/clients/%d", clientID) {
		t.Fatalf("contact by phone = %+v, want the client's contact", byPhone)
	}
	byTag := groupOf(search(member, url.Values{"q": {"постоян"}}), "clients")
	if byTag == nil || len(byTag.Items) != 1 || byTag.Items[0].Href != fmt.Sprintf("/clients/%d", clientID) {
		t.Fatalf("client by tag = %+v, want the tagged client", byTag)
	}
	if groupOf(search(member, url.Values{"q": {"buh@romashka"}}), "clients") != nil {
		t.Fatal("a member found the client by its billing email")
	}
	byEmail := groupOf(search(owner, url.Values{"q": {"buh@romashka"}}), "clients")
	if byEmail == nil || len(byEmail.Items) != 1 || !strings.Contains(byEmail.Items[0].Detail, "buh@romashka.test") {
		t.Fatalf("manager's search by email = %+v, want the email shown", byEmail)
	}
	if tagged := groupOf(search(member, url.Values{"q": {"ромаш"}, "tag": {"постоянный"}}), "clients"); tagged == nil || len(tagged.Items) != 1 {
		t.Fatal("the tag filter lost the tagged client")
	}
	if untagged := groupOf(search(member, url.Values{"q": {"ромаш"}, "tag": {"оптовый"}}), "clients"); untagged != nil {
		t.Fatal("the tag filter kept a client without the tag")
	}

	short := readBody(t, member.do("GET", "/api/search?q=a&limit=50", nil, map[string]string{"Accept": "application/json"}))
	if !strings.Contains(strings.Join(strings.Fields(short), ""), `"groups":[]`) {
		t.Fatalf("a one-letter search answered groups: %s", short)
	}
}

// The agent reads clients and contacts with clients_search and sets tags with
// client_tags_set. A manager's call replaces the set, a member's call gets the forbidden
// text, a call without tags is refused rather than clearing them, and the billing email
// reaches the manager only.
func TestMCPClientsSearchAndClientTagsSet(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "mcp-client-search")
	clientID := insertClientInTeam(t, owner, mcpStudioID(t, owner), "Василёк")
	if _, err := owner.db.TestSQL().ExecContext(t.Context(), `UPDATE clients SET email = ? WHERE id = ?`, "buh@vasilek.test", clientID); err != nil {
		t.Fatal(err)
	}
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "agent"))

	mcpMustCall(t, owner, ownerAuth, "client_tags_set", map[string]any{"client_id": clientID, "tags": []string{"Оптовый", "vip"}})

	text, failed := callTool(t, member, memberAuth, "client_tags_set", map[string]any{"client_id": clientID, "tags": []string{}})
	if !failed || !slices.Contains(forbiddenTexts(t), text) {
		t.Fatalf("member sets tags over MCP: failed %v, text %q; want the forbidden text", failed, text)
	}
	if text, failed := callTool(t, owner, ownerAuth, "client_tags_set", map[string]any{"client_id": clientID}); !failed || !strings.Contains(text, "tags") {
		t.Fatalf("a call without tags: failed %v, text %q; want a refusal naming tags", failed, text)
	}

	type searchClient struct {
		ID    int64    `json:"id"`
		Email string   `json:"email"`
		Tags  []string `json:"tags"`
	}
	type searchAnswer struct {
		Clients  []searchClient `json:"clients"`
		Contacts []any          `json:"contacts"`
	}
	var asMember, asOwner searchAnswer
	if err := json.Unmarshal([]byte(mcpMustCall(t, member, memberAuth, "clients_search", map[string]any{"q": "оптов"})), &asMember); err != nil {
		t.Fatal(err)
	}
	if len(asMember.Clients) != 1 || asMember.Clients[0].ID != clientID || asMember.Clients[0].Email != "" || !slices.Equal(asMember.Clients[0].Tags, []string{"vip", "оптовый"}) {
		t.Fatalf("member's clients_search = %+v, want the client with its tags and no email", asMember.Clients)
	}
	if err := json.Unmarshal([]byte(mcpMustCall(t, owner, ownerAuth, "clients_search", map[string]any{"q": "оптов"})), &asOwner); err != nil {
		t.Fatal(err)
	}
	if len(asOwner.Clients) != 1 || asOwner.Clients[0].Email != "buh@vasilek.test" {
		t.Fatalf("manager's clients_search = %+v, want the billing email", asOwner.Clients)
	}
	if short := mcpMustCall(t, member, memberAuth, "clients_search", map[string]any{"q": "в"}); !strings.Contains(short, `"clients":[]`) || !strings.Contains(short, `"contacts":[]`) {
		t.Fatalf("a one-letter clients_search answered %s", short)
	}
}
