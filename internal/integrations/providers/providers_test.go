package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpurl"
	"github.com/aa-blinov/paratrack/internal/integrationport"
	"github.com/aa-blinov/paratrack/internal/integrations/providers"
)

type extItem = providers.Task

func fetchProviderTest(client *http.Client, provider, secret, target string) ([]extItem, error) {
	return fetchProviderTestWithWaits(client, []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}, provider, secret, target)
}

func fetchProviderTestWithWaits(client *http.Client, waits []time.Duration, provider, secret, target string) ([]extItem, error) {
	providerClient, err := providers.NewWithRetryWaits(client, waits)
	if err != nil {
		return nil, err
	}
	input, err := providerClient.FetchTasks(context.Background(), integrationport.ProviderInput{
		Provider: provider, Secret: secret, Config: integrationport.ProviderConfig{Target: target},
	})
	if err != nil {
		return nil, err
	}
	items := make([]extItem, 0, len(input))
	for _, item := range input {
		items = append(items, extItem{ID: item.ExternalID, Title: item.Title, URL: item.URL, Status: item.Status})
	}
	return items, nil
}

func fakeProviders(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: providerTestTransport{target: target}}
}

type providerTestTransport struct{ target *url.URL }

func (rt providerTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("X-Orig-Host", request.URL.Host)
	request.URL.Scheme, request.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(request)
}

func TestProviderClientsFollowPagination(t *testing.T) {
	cases := []struct {
		name  string
		serve func(w http.ResponseWriter, r *http.Request) bool // true = was the 2nd page
		fetch func(*http.Client) ([]extItem, error)
	}{
		{"github Link rel=next", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"number":2,"title":"b","repository":{"full_name":"o/r"}}]`)
				return true
			}
			w.Header().Set("Link", `<https://api.github.com/issues?page=2>; rel="next", <https://api.github.com/issues?page=2>; rel="last"`)
			fmt.Fprint(w, `[{"number":1,"title":"a","repository":{"full_name":"o/r"}}]`)
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "github", "t", "") }},
		{"gitlab Link rel=next", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"iid":2,"title":"b"}]`)
				return true
			}
			w.Header().Set("Link", `<https://gitlab.com/api/v4/projects/g%2Fp/issues?page=2&per_page=100>; rel="next"`)
			fmt.Fprint(w, `[{"iid":1,"title":"a"}]`)
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "gitlab", "t", "g/p") }},
		{"jira nextPageToken", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("nextPageToken") == "tok2" {
				fmt.Fprint(w, `{"issues":[{"key":"P-2","fields":{"summary":"b"}}],"isLast":true}`)
				return true
			}
			fmt.Fprint(w, `{"issues":[{"key":"P-1","fields":{"summary":"a"}}],"nextPageToken":"tok2","isLast":false}`)
			return false
		}, func(client *http.Client) ([]extItem, error) {
			return fetchProviderTest(client, "jira", "e:k", "https://acme.atlassian.net PROJ")
		}},
		{"asana next_page.offset", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("offset") == "off2" {
				fmt.Fprint(w, `{"data":[{"gid":"2","name":"b"}],"next_page":null}`)
				return true
			}
			fmt.Fprint(w, `{"data":[{"gid":"1","name":"a"}],"next_page":{"offset":"off2"}}`)
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "asana", "t", "123") }},
		{"clickup page + last_page", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("page") == "1" {
				fmt.Fprint(w, `{"tasks":[{"id":"z","name":"last"}],"last_page":true}`)
				return true
			}
			tasks := make([]string, 100)
			for i := range tasks {
				tasks[i] = fmt.Sprintf(`{"id":"t%d","name":"n"}`, i)
			}
			fmt.Fprintf(w, `{"tasks":[%s],"last_page":false}`, strings.Join(tasks, ","))
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "clickup", "pk", "L1") }},
		{"todoist next_cursor", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("cursor") == "c2" {
				fmt.Fprint(w, `{"results":[{"id":"2","content":"b"}],"next_cursor":null}`)
				return true
			}
			fmt.Fprint(w, `{"results":[{"id":"1","content":"a"}],"next_cursor":"c2"}`)
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "todoist", "t", "") }},
		{"notion data source + start_cursor", func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == "GET" {
				if r.Header.Get("Notion-Version") != providers.NotionVersion {
					w.WriteHeader(400)
				}
				fmt.Fprint(w, `{"data_sources":[{"id":"ds1"}]}`)
				return false
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !strings.HasPrefix(r.URL.Path, "/v1/data_sources/ds1/query") {
				w.WriteHeader(404)
				return false
			}
			if body["start_cursor"] == "cur2" {
				fmt.Fprint(w, `{"results":[{"id":"p2","properties":{"Name":{"title":[{"plain_text":"b"}]}}}],"has_more":false}`)
				return true
			}
			fmt.Fprint(w, `{"results":[{"id":"p1","properties":{"Name":{"title":[{"plain_text":"a"}]}}}],"has_more":true,"next_cursor":"cur2"}`)
			return false
		}, func(client *http.Client) ([]extItem, error) {
			return fetchProviderTest(client, "notion", "s", "0123456789abcdef0123456789abcdef")
		}},
		{"trello before=<last id>", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("before") == "c999" {
				fmt.Fprint(w, `[{"id":"old","name":"old"}]`)
				return true
			}
			cards := make([]string, 1000)
			for i := range cards {
				cards[i] = fmt.Sprintf(`{"id":"c%d","name":"n"}`, i)
			}
			fmt.Fprintf(w, `[%s]`, strings.Join(cards, ","))
			return false
		}, func(client *http.Client) ([]extItem, error) { return fetchProviderTest(client, "trello", "k:t", "B1") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			second := false
			client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
				if c.serve(w, r) {
					second = true
				}
			})
			items, err := c.fetch(client)
			if err != nil {
				t.Fatal(err)
			}
			if !second {
				t.Fatalf("second page never requested (%d items)", len(items))
			}
			seen := map[string]bool{}
			for _, it := range items {
				if seen[it.ID] {
					t.Fatalf("duplicate id %s", it.ID)
				}
				seen[it.ID] = true
			}
		})
	}
}

// A 429 is retried after Retry-After; a plain error isn't.

func TestProviderClientsRetryRateLimits(t *testing.T) {
	calls := 0
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			io.WriteString(w, `{"error":"slow down"}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":"1","content":"a"}],"next_cursor":null}`)
	})
	if items, err := fetchProviderTestWithWaits(client, []time.Duration{0, 0, 0}, "todoist", "t", ""); err != nil || len(items) != 1 || calls != 2 {
		t.Fatalf("items %d err %v calls %d", len(items), err, calls)
	}
	calls = 0
	client = fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(401) })
	if _, err := fetchProviderTestWithWaits(client, []time.Duration{0, 0, 0}, "todoist", "t", ""); err == nil || calls != 1 || !strings.Contains(err.Error(), "todoist 401") {
		t.Fatalf("401 must fail once with a clear error: err %v calls %d", err, calls)
	}
}

// Toggl: older ranges go through Reports v3, paged by X-Next-Row-Number.

func TestGitHubRejectsCrossOriginPagination(t *testing.T) {
	requests := 0
	client := fakeProviders(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Link", `<https://attacker.example.test/collect>; rel="next"`)
		fmt.Fprint(w, `[]`)
	})
	_, err := fetchProviderTest(client, "github", "sensitive-token", "owner/repo")
	if !errors.Is(err, httpurl.ErrUnsafeURL) {
		t.Fatalf("FetchTasks error = %v, want ErrUnsafeURL", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want exactly one request before rejecting the foreign link", requests)
	}
}

// Clockify stops on its Last-Page header.

func TestJiraDataCenterPaging(t *testing.T) {
	var starts []string
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/search" || r.Header.Get("Authorization") != "Bearer pat" {
			w.WriteHeader(401)
			return
		}
		starts = append(starts, r.URL.Query().Get("startAt"))
		if r.URL.Query().Get("startAt") == "0" {
			fmt.Fprint(w, `{"startAt":0,"total":2,"issues":[{"key":"A-1","fields":{"summary":"x"}}]}`)
			return
		}
		fmt.Fprint(w, `{"startAt":1,"total":2,"issues":[{"key":"A-2","fields":{"summary":"y"}}]}`)
	})
	got, err := fetchProviderTest(client, "jira", "pat", "https://jira.acme.ru A")
	if err != nil || len(got) != 2 || strings.Join(starts, ",") != "0,1" {
		t.Fatalf("items %d err %v startAt %v", len(got), err, starts)
	}
}

func TestCustomProviderSitesRequireHTTPS(t *testing.T) {
	client := fakeProviders(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("invalid provider site reached the HTTP client")
	})
	for _, test := range []struct {
		provider string
		target   string
	}{
		{provider: "jira", target: "http://127.0.0.1:8080 PROJ"},
		{provider: "gitlab", target: "http://127.0.0.1:8080 group/project"},
	} {
		t.Run(test.provider, func(t *testing.T) {
			if _, err := fetchProviderTest(client, test.provider, "sensitive-token", test.target); err == nil {
				t.Fatal("insecure custom provider site accepted")
			}
		})
	}
}

func TestJiraSearchJQL(t *testing.T) {
	var gotPath, gotJQL, gotHost string
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotJQL, gotHost = r.URL.Path, r.URL.Query().Get("jql"), r.Header.Get("X-Orig-Host")
		fmt.Fprint(w, `{"issues":[{"key":"PROJ-1","fields":{"summary":"Fix","status":{"name":"To Do"}}}]}`)
	})
	if _, err := fetchProviderTest(client, "jira", "a:b", "PROJ"); err == nil {
		t.Fatal("no site anywhere must be an error")
	}
	items, err := fetchProviderTest(client, "jira", "a:b", "https://acme.atlassian.net PROJ")
	if err != nil || len(items) != 1 || items[0].URL != "https://acme.atlassian.net/browse/PROJ-1" {
		t.Fatalf("items %+v err %v", items, err)
	}
	if gotHost != "acme.atlassian.net" || gotPath != "/rest/api/3/search/jql" || !strings.HasPrefix(gotJQL, "project = PROJ AND") {
		t.Errorf("host %q path %q jql %q", gotHost, gotPath, gotJQL)
	}
	for _, raw := range []string{"project=PROJ", "assignee in (currentUser())"} {
		fetchProviderTest(client, "jira", "a:b", "https://acme.atlassian.net "+raw)
		if gotJQL != raw {
			t.Errorf("raw JQL %q was rewritten to %q", raw, gotJQL)
		}
	}
}

// GitHub's issues API returns pull requests too, and issue numbers repeat
// across repos.

func TestGitHubSkipsPRsAndKeysByRepo(t *testing.T) {
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"number":1,"title":"a","repository":{"full_name":"o/x"}},
			{"number":1,"title":"b","repository":{"full_name":"o/y"}},
			{"number":2,"title":"pr","pull_request":{},"repository":{"full_name":"o/x"}}]`)
	})
	items, err := fetchProviderTest(client, "github", "t", "")
	if err != nil || len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestTodoistLinkWithoutURLField(t *testing.T) {
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"id":"99","content":"call"}]}`)
	})
	items, err := fetchProviderTest(client, "todoist", "t", "")
	if err != nil || len(items) != 1 || items[0].URL != "https://app.todoist.com/app/task/99" {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestNotionDatabaseIDFormat(t *testing.T) {
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	_, err := fetchProviderTest(client, "notion", "secret", "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "32 hex") {
		t.Fatalf("err=%v", err)
	}
	// well-formed id should reach the network layer (we only check parse)
	// by asserting the error is NOT a format error.
	_, err = fetchProviderTest(client, "notion", "secret", "00000000000000000000000000000000")
	if err != nil && strings.Contains(err.Error(), "32 hex") {
		t.Fatalf("id parsed but rejected: %v", err)
	}
}

func TestNewProviderGuards(t *testing.T) {
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	// asana / gitlab / clickup empty-target errors
	if _, err := fetchProviderTest(client, "asana", "t", ""); err == nil || !strings.Contains(err.Error(), "project gid") {
		t.Fatalf("asana: %v", err)
	}
	if _, err := fetchProviderTest(client, "gitlab", "t", ""); err == nil || !strings.Contains(err.Error(), "group/project") {
		t.Fatalf("gitlab: %v", err)
	}
	if _, err := fetchProviderTest(client, "clickup", "t", ""); err == nil || !strings.Contains(err.Error(), "list id") {
		t.Fatalf("clickup: %v", err)
	}
	// todoist accepts empty target (all tasks) — just ensure it builds a request
	// (network may 401, that's fine).
	_, err := fetchProviderTest(client, "todoist", "t", "")
	if err != nil && strings.Contains(err.Error(), "required") {
		t.Fatalf("todoist should not require target: %v", err)
	}
}

// Read-only tokens can't write; expired tokens don't authenticate; a
// token acts in the workspace it was made in.
