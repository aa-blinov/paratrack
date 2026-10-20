package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

const githubBoardFirstPage = `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
 {"id":"PVTI_1","fieldValueByName":{"name":"In Progress"},"content":{"__typename":"Issue","number":12,"title":"Fix login","url":"https://github.com/acme/site/issues/12","state":"OPEN","repository":{"nameWithOwner":"acme/site"}}},
 {"id":"PVTI_2","fieldValueByName":{"name":"Done"},"content":{"__typename":"DraftIssue","title":"Think about pricing"}}
]}}}}}`

const githubBoardLastPage = `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[
 {"id":"PVTI_3","fieldValueByName":{"name":"In Progress"},"content":{"__typename":"PullRequest","number":40,"title":"Move billing","url":"https://github.com/acme/site/pull/40","state":"MERGED","repository":{"nameWithOwner":"acme/site"}}},
 {"id":"PVTI_4","fieldValueByName":null,"content":null}
]}}}}}`

// The reader follows the cursor across pages, sends the token as a bearer header,
// keeps the draft and the issue apart by reference, and reads a merged pull
// request as closed.
func TestFetchGitHubProjectReadsEveryPage(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !strings.Contains(body.Query, "organization(login: $login)") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		pages++
		if body.Variables["cursor"] == nil {
			fmt.Fprint(w, githubBoardFirstPage)
			return
		}
		fmt.Fprint(w, githubBoardLastPage)
	}))
	defer server.Close()

	client, err := NewWithConfigAndRetryWaits(server.Client(), nil, Config{GitHubGraphQL: server.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	items, err := client.FetchGitHubProject(t.Context(), "tok", "https://github.com/orgs/acme/projects/3")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if pages != 2 || len(items) != 3 {
		t.Fatalf("read %d pages and %d items, want 2 and 3 (the empty item is skipped)", pages, len(items))
	}
	want := []appmodel.GitHubProjectItem{
		{Ref: "https://github.com/acme/site/issues/12", Title: "acme/site#12 Fix login", URL: "https://github.com/acme/site/issues/12", Status: "In Progress"},
		{Ref: "github-draft:PVTI_2", Title: "Think about pricing", Status: "Done"},
		{Ref: "https://github.com/acme/site/pull/40", Title: "acme/site#40 Move billing", URL: "https://github.com/acme/site/pull/40", Status: "In Progress", Closed: true},
	}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("item %d = %+v, want %+v", i, items[i], want[i])
		}
	}
}

// A link that is not a Projects board never reaches the network, and a board the
// token cannot read says so in terms the import can show.
func TestFetchGitHubProjectRefusesWhatItCannotRead(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":{"organization":null,"user":null}}`)
	}))
	defer server.Close()
	client, err := NewWithConfigAndRetryWaits(server.Client(), nil, Config{GitHubGraphQL: server.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	if _, err := client.FetchGitHubProject(t.Context(), "tok", "https://github.com/acme/site/issues"); err != appmodel.ErrGitHubProjectURL {
		t.Fatalf("a repository link: %v, want ErrGitHubProjectURL", err)
	}
	if calls != 0 {
		t.Fatalf("a refused link still made %d requests", calls)
	}
	if _, err := client.FetchGitHubProject(t.Context(), "tok", "https://github.com/orgs/acme/projects/3"); err != appmodel.ErrGitHubProjectNotFound {
		t.Fatalf("a board GitHub does not return: %v, want ErrGitHubProjectNotFound", err)
	}
}
