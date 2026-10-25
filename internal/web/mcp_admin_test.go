package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The administration reads follow their pages: webhooks and the settings and invitations
// screens are refused to a member, and the integrations list is the one a member can read.
func TestMCPAdministrationReadsAreRefusedToMembers(t *testing.T) {
	_, member := studioOwnerAndMember(t, "admin-refused")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	for _, name := range []string{"webhooks_list", "workspace_settings_get", "invitations_list"} {
		text, failed := callTool(t, member, memberAuth, name, map[string]any{})
		if !failed || !strings.Contains(text, "прав") {
			t.Fatalf("a member called %s: failed=%v text=%q, want the forbidden text", name, failed, text)
		}
	}
	if _, failed := callTool(t, member, memberAuth, "integrations_list", map[string]any{}); failed {
		t.Fatal("a member could not read the integrations list, which the integrations page shows to every member")
	}
}

// The owner's webhook list carries the endpoint, and never the signing secret the webhook was made with.
func TestMCPOwnerListsWebhooksWithoutTheSecret(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "admin-webhooks")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	const secret = "whsec_MCP_TEST_SECRET_9f2c"
	const endpoint = "https://hooks.example.test/paratrack"

	resp := owner.do("POST", "/api/webhooks", url.Values{"url": {endpoint}, "secret": {secret}}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create the webhook: status %d", resp.StatusCode)
	}

	text := mcpMustCall(t, owner, auth, "webhooks_list", map[string]any{})
	if !strings.Contains(text, endpoint) || !strings.Contains(text, `"active":true`) {
		t.Fatalf("webhooks_list = %q, want the endpoint", text)
	}
	if strings.Contains(text, secret) || strings.Contains(text, "secret") {
		t.Fatalf("webhooks_list leaks the signing secret: %q", text)
	}
}

// A member's token list holds the member's own tokens only, and never a token value.
func TestMCPAPITokensListShowsOnlyTheCallersTokens(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "admin-tokens")
	apiTokenOf(t, owner, "owner-agent")
	memberToken := apiTokenOf(t, member, "member-agent")

	text := mcpMustCall(t, member, mcpBearer(memberToken), "api_tokens_list", map[string]any{})
	if !strings.Contains(text, `"name":"member-agent"`) || strings.Contains(text, "owner-agent") {
		t.Fatalf("api_tokens_list for a member = %q, want only the member's token", text)
	}
	if strings.Contains(text, memberToken) {
		t.Fatalf("api_tokens_list returns a token value: %q", text)
	}
}

// task_get shows a task's feed and checklist, including what was added before the read.
func TestMCPTaskGetReturnsTheFeedAndTheChecklist(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "admin-task")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	projectID := mcpID(t, mcpMustCall(t, owner, auth, "project_create", map[string]any{"name": "Фид"}))
	taskID := mcpID(t, mcpMustCall(t, owner, auth, "task_create", map[string]any{"project_id": projectID, "title": "Лендинг"}))
	mcpMustCall(t, owner, auth, "task_comment_add", map[string]any{"task_id": taskID, "body": "Макет согласован"})
	mcpMustCall(t, owner, auth, "task_checklist_add", map[string]any{"task_id": taskID, "text": "Проверить верстку"})

	var view struct {
		Title    string `json:"title"`
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
		Checklist []struct {
			Text string `json:"text"`
			Done bool   `json:"done"`
		} `json:"checklist"`
	}
	if err := json.Unmarshal([]byte(mcpMustCall(t, owner, auth, "task_get", map[string]any{"task_id": taskID})), &view); err != nil {
		t.Fatalf("task_get is not a task: %v", err)
	}
	if view.Title != "Лендинг" || len(view.Comments) != 1 || view.Comments[0].Body != "Макет согласован" ||
		len(view.Checklist) != 1 || view.Checklist[0].Text != "Проверить верстку" || view.Checklist[0].Done {
		t.Fatalf("task_get = %+v, want the title, the comment and the open checklist step", view)
	}
	if _, failed := callTool(t, owner, auth, "task_get", map[string]any{"task_id": 999999}); !failed {
		t.Fatal("task_get of a missing task did not fail")
	}
}

// The owner reads the workspace settings and the invitations, and neither carries a link or a key.
func TestMCPOwnerReadsSettingsAndInvitationsWithoutSecrets(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "admin-settings")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))

	resp := owner.do("POST", "/api/team/invites", nil, nil)
	loc, _ := url.QueryUnescape(resp.Header.Get("Location"))
	resp.Body.Close()
	link := regexp.MustCompile(`/invites/([A-Za-z0-9_-]+)`).FindStringSubmatch(loc)
	if link == nil {
		t.Fatalf("no invite link in %q", loc)
	}

	settings := mcpMustCall(t, owner, auth, "workspace_settings_get", map[string]any{})
	if !strings.Contains(settings, `"name":"Студия"`) || !strings.Contains(settings, `"stripe_key_set":false`) {
		t.Fatalf("workspace_settings_get = %q, want the name and no Stripe key", settings)
	}

	invitations := mcpMustCall(t, owner, auth, "invitations_list", map[string]any{})
	if !strings.Contains(invitations, `"state":"open"`) || !strings.Contains(invitations, `"state":"accepted"`) {
		t.Fatalf("invitations_list = %q, want an open and an accepted invitation", invitations)
	}
	if strings.Contains(invitations, link[1]) {
		t.Fatalf("invitations_list returns the invitation link: %q", invitations)
	}
}
