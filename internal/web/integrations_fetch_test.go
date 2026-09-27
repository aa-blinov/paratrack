package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Every importer must read past the first page, the way the vendor
// documents it. Each fake serves two pages and records the second call.
func TestImportersFollowPagination(t *testing.T) {
	cases := []struct {
		name  string
		serve func(w http.ResponseWriter, r *http.Request) bool // true = was the 2nd page
		fetch func() ([]extItem, error)
	}{
		{"github Link rel=next", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"number":2,"title":"b","repository":{"full_name":"o/r"}}]`)
				return true
			}
			w.Header().Set("Link", `<https://api.github.com/issues?page=2>; rel="next", <https://api.github.com/issues?page=2>; rel="last"`)
			fmt.Fprint(w, `[{"number":1,"title":"a","repository":{"full_name":"o/r"}}]`)
			return false
		}, func() ([]extItem, error) { return fetchGitHubIssues("t", "") }},
		{"gitlab Link rel=next", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"iid":2,"title":"b"}]`)
				return true
			}
			w.Header().Set("Link", `<https://gitlab.com/api/v4/projects/g%2Fp/issues?page=2&per_page=100>; rel="next"`)
			fmt.Fprint(w, `[{"iid":1,"title":"a"}]`)
			return false
		}, func() ([]extItem, error) { return fetchGitLabIssues("t", "g/p") }},
		{"jira nextPageToken", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("nextPageToken") == "tok2" {
				fmt.Fprint(w, `{"issues":[{"key":"P-2","fields":{"summary":"b"}}],"isLast":true}`)
				return true
			}
			fmt.Fprint(w, `{"issues":[{"key":"P-1","fields":{"summary":"a"}}],"nextPageToken":"tok2","isLast":false}`)
			return false
		}, func() ([]extItem, error) { return fetchJiraIssues("e:k", "https://acme.atlassian.net PROJ") }},
		{"asana next_page.offset", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("offset") == "off2" {
				fmt.Fprint(w, `{"data":[{"gid":"2","name":"b"}],"next_page":null}`)
				return true
			}
			fmt.Fprint(w, `{"data":[{"gid":"1","name":"a"}],"next_page":{"offset":"off2"}}`)
			return false
		}, func() ([]extItem, error) { return fetchAsanaTasks("t", "123") }},
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
		}, func() ([]extItem, error) { return fetchClickUpTasks("pk", "L1") }},
		{"todoist next_cursor", func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("cursor") == "c2" {
				fmt.Fprint(w, `{"results":[{"id":"2","content":"b"}],"next_cursor":null}`)
				return true
			}
			fmt.Fprint(w, `{"results":[{"id":"1","content":"a"}],"next_cursor":"c2"}`)
			return false
		}, func() ([]extItem, error) { return fetchTodoistTasks("t", "") }},
		{"notion data source + start_cursor", func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == "GET" {
				if r.Header.Get("Notion-Version") != notionVersion {
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
		}, func() ([]extItem, error) { return fetchNotionTasks("s", "0123456789abcdef0123456789abcdef") }},
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
		}, func() ([]extItem, error) { return fetchTrelloCards("k:t", "B1") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			second := false
			fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
				if c.serve(w, r) {
					second = true
				}
			})
			items, err := c.fetch()
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
func TestImporterRetriesRateLimit(t *testing.T) {
	old := retryWaits
	retryWaits = []time.Duration{0, 0, 0}
	defer func() { retryWaits = old }()
	calls := 0
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			io.WriteString(w, `{"error":"slow down"}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":"1","content":"a"}],"next_cursor":null}`)
	})
	if items, err := fetchTodoistTasks("t", ""); err != nil || len(items) != 1 || calls != 2 {
		t.Fatalf("items %d err %v calls %d", len(items), err, calls)
	}
	calls = 0
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(401) })
	if _, err := fetchTodoistTasks("t", ""); err == nil || calls != 1 || !strings.Contains(err.Error(), "todoist 401") {
		t.Fatalf("401 must fail once with a clear error: err %v calls %d", err, calls)
	}
}

// Toggl: older ranges go through Reports v3, paged by X-Next-Row-Number.
func TestTogglReportsPagination(t *testing.T) {
	var rows []string
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v9/me":
			fmt.Fprint(w, `{"default_workspace_id":7}`)
		case r.URL.Path == "/reports/api/v3/workspace/7/search/time_entries":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			rows = append(rows, fmt.Sprint(b["first_row_number"]))
			if b["first_row_number"] == nil {
				w.Header().Set("X-Next-Row-Number", "51")
				fmt.Fprint(w, `[{"description":"a","time_entries":[{"id":1,"start":"2025-01-02T09:00:00Z","stop":"2025-01-02T10:00:00Z"}]}]`)
				return
			}
			fmt.Fprint(w, `[{"description":"b","time_entries":[{"id":2,"start":"2025-01-03T09:00:00Z","stop":"2025-01-03T09:30:00Z"},{"id":3,"start":"2025-01-03T11:00:00Z","stop":""}]}]`)
		default:
			w.WriteHeader(404)
		}
	})
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := fetchTogglEntries("t", from, from.AddDate(0, 1, 0))
	if err != nil || len(got) != 2 {
		t.Fatalf("entries %+v err %v", got, err)
	}
	if len(rows) != 2 || rows[1] != "51" {
		t.Errorf("first_row_number sequence %v", rows)
	}
}

// Clockify stops on its Last-Page header.
func TestClockifyLastPageHeader(t *testing.T) {
	pages := 0
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/user" {
			fmt.Fprint(w, `{"id":"u","activeWorkspace":"w"}`)
			return
		}
		pages++
		entries := make([]string, 200)
		for i := range entries {
			entries[i] = fmt.Sprintf(`{"id":"e%d-%d","timeInterval":{"start":"2026-09-01T09:00:00Z","end":"2026-09-01T09:10:00Z"}}`, pages, i)
		}
		if pages == 2 {
			w.Header().Set("Last-Page", "true")
		}
		fmt.Fprintf(w, "[%s]", strings.Join(entries, ","))
	})
	got, err := fetchClockifyEntries("k", "", time.Now().AddDate(0, 0, -7), time.Now())
	if err != nil || pages != 2 || len(got) != 400 {
		t.Fatalf("pages %d entries %d err %v", pages, len(got), err)
	}
}
