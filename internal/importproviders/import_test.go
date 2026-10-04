package importproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpurl"
	"github.com/aa-blinov/paratrack/internal/importport"
)

func fakeImportProviders(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: importTestTransport{target: target}}
}

type importTestTransport struct{ target *url.URL }

func (rt importTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme, request.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(request)
}

func TestParseImportRangeUsesInjectedNow(t *testing.T) {
	now := time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC)
	from, to, err := parseImportRange("", "", "", "UTC", now)
	if err != nil {
		t.Fatalf("parseImportRange: %v", err)
	}
	if want := now.AddDate(0, 0, -30); !from.Equal(want) {
		t.Fatalf("default from = %s, want %s", from, want)
	}
	if !to.Equal(now) {
		t.Fatalf("default to = %s, want %s", to, now)
	}
}

func TestParseImportRangeUsesUserCalendarZone(t *testing.T) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)
	from, to, err := parseImportRange("2026-09-01", "2026-09-01", "Asia/Almaty", "UTC", now)
	if err != nil {
		t.Fatal(err)
	}
	if from.Location().String() != "Asia/Almaty" || to.Sub(from) != 24*time.Hour || from.UTC().Hour() != 19 {
		t.Fatalf("from %v to %v", from, to)
	}
}

func TestTogglReportsPagination(t *testing.T) {
	var rows []string
	client := fakeImportProviders(t, func(w http.ResponseWriter, r *http.Request) {
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
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importer.Fetch(context.Background(), importport.ProviderRequest{
		Provider: "toggl", Secret: "t", From: from.Format("2006-01-02"), To: from.AddDate(0, 1, 0).Format("2006-01-02"),
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("entries %+v err %v", got, err)
	}
	if len(rows) != 2 || rows[1] != "51" {
		t.Errorf("first_row_number sequence %v", rows)
	}
}

func TestClockifyLastPageHeader(t *testing.T) {
	pages := 0
	client := fakeImportProviders(t, func(w http.ResponseWriter, r *http.Request) {
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
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	from, to := time.Now().AddDate(0, 0, -7), time.Now()
	got, err := importer.Fetch(context.Background(), importport.ProviderRequest{
		Provider: "clockify", Secret: "k", From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
	})
	if err != nil || pages != 2 || len(got) != 400 {
		t.Fatalf("pages %d entries %d err %v", pages, len(got), err)
	}
}

// Jira Data Center: a bare PAT goes to /rest/api/2/search, paged by startAt.

func TestFetchEntriesGuards(t *testing.T) {
	// missing extra fields
	importer, err := New(&http.Client{}, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Fetch(context.Background(), importport.ProviderRequest{Provider: "harvest", Secret: "tok"}); err == nil ||
		!strings.Contains(err.Error(), "account id") {
		t.Fatalf("harvest: %v", err)
	}
	if _, err := importer.Fetch(context.Background(), importport.ProviderRequest{Provider: "nope"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestClockifyUserEntries(t *testing.T) {
	var paths []string
	client := fakeImportProviders(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/user":
			fmt.Fprint(w, `{"id":"u1","activeWorkspace":"ws1"}`)
		default:
			fmt.Fprint(w, `[{"id":"e1","description":"design","timeInterval":{"start":"2026-09-01T09:00:00Z","end":"2026-09-01T10:00:00Z"}},
				{"id":"e2","description":"live","timeInterval":{"start":"2026-09-01T11:00:00Z","end":""}}]`)
		}
	})
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importer.Fetch(context.Background(), importport.ProviderRequest{Provider: "clockify", Secret: "key", To: "2026-09-01"})
	if err != nil || len(got) != 1 || got[0].ExternalID != "clockify:e1" {
		t.Fatalf("entries %+v err %v", got, err)
	}
	if len(paths) < 2 || paths[1] != "/api/v1/workspaces/ws1/user/u1/time-entries" {
		t.Errorf("paths %v", paths)
	}
}

// Harvest: every page, inclusive "to", real clock times, no running entries.

func TestHarvestPagesAndTimes(t *testing.T) {
	var firstTo string
	client := fakeImportProviders(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			firstTo = r.URL.Query().Get("to")
			fmt.Fprint(w, `{"time_entries":[{"id":1,"notes":"a","hours":1.5,"spent_date":"2026-09-01","started_time":"8:00am"},
				{"id":2,"notes":"run","hours":1,"spent_date":"2026-09-01","is_running":true}],
				"links":{"next":"https://api.harvestapp.com/v2/time_entries?page=2"}}`)
			return
		}
		fmt.Fprint(w, `{"time_entries":[{"id":3,"notes":"b","hours":2,"spent_date":"2026-09-02"}],"links":{"next":null}}`)
	})
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importer.Fetch(context.Background(), importport.ProviderRequest{
		Provider: "harvest", Secret: "t", Extra: "acc", From: "2026-09-01", To: "2026-09-02",
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("entries %+v err %v", got, err)
	}
	if firstTo != "2026-09-02" {
		t.Errorf("to = %s, want the inclusive last day 2026-09-02", firstTo)
	}
	if got[0].Start.Hour() != 8 || got[0].End.Sub(got[0].Start) != 90*time.Minute {
		t.Errorf("clock time not used: %v – %v", got[0].Start, got[0].End)
	}
}

func TestHarvestRejectsCrossOriginPagination(t *testing.T) {
	requests := 0
	client := fakeImportProviders(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		fmt.Fprint(w, `{"time_entries":[],"links":{"next":"https://attacker.example.test/collect"}}`)
	})
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = importer.Fetch(context.Background(), importport.ProviderRequest{
		Provider: "harvest", Secret: "sensitive-token", Extra: "account", From: "2026-09-01", To: "2026-09-02",
	})
	if !errors.Is(err, httpurl.ErrUnsafeURL) {
		t.Fatalf("Fetch error = %v, want ErrUnsafeURL", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want exactly one request before rejecting the foreign link", requests)
	}
}

func TestHarvestRejectsUnrepresentableDuration(t *testing.T) {
	client := fakeImportProviders(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"time_entries":[{"id":1,"notes":"a","hours":1e20,"spent_date":"2026-09-01"}],"links":{"next":null}}`)
	})
	importer, err := New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Fetch(context.Background(), importport.ProviderRequest{
		Provider: "harvest", Secret: "t", Extra: "acc", From: "2026-09-01", To: "2026-09-01",
	}); err == nil {
		t.Fatal("unrepresentable Harvest duration unexpectedly accepted")
	}
}

func TestAppendImportedEntryEnforcesLimit(t *testing.T) {
	entries := make([]importedEntry, importport.MaxEntries)
	if _, err := appendImportedEntry(entries, importedEntry{}); !errors.Is(err, ErrEntryLimit) {
		t.Fatalf("appendImportedEntry error = %v, want ErrEntryLimit", err)
	}
}
