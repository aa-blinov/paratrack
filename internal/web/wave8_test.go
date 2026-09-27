package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestImportRange(t *testing.T) {
	from, to, err := parseImportRange("2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if !to.After(from) {
		t.Fatalf("range inverted: %v %v", from, to)
	}
	if _, _, err := parseImportRange("2026-09-30", "2026-09-01"); err == nil {
		t.Fatal("bad range accepted")
	}
	if _, _, err := parseImportRange("nope", ""); err == nil {
		t.Fatal("bad from accepted")
	}
	// defaults
	from, to, _ = parseImportRange("", "")
	if !to.After(from) {
		t.Fatal("defaults inverted")
	}
}

func TestFetchEntriesGuards(t *testing.T) {
	// missing extra fields
	if _, err := fetchHarvestEntries("tok", "", time.Now(), time.Now()); err == nil ||
		!strings.Contains(err.Error(), "account id") {
		t.Fatalf("harvest: %v", err)
	}
	if _, err := fetchEntries("nope", "", "", "", ""); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestImportPageAndRun(t *testing.T) {
	e := newAPIEnv(t)
	e.register("import@x.test")
	// page renders
	resp := e.do("GET", "/import", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("import page: %d %s", resp.StatusCode, readBody(t, resp))
	}
	body := readBody(t, resp)
	for _, want := range []string{"Toggl", "Harvest", "Clockify"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing provider %s", want)
		}
	}
	// run with unknown provider → redirect back with error
	resp = e.do("POST", "/import/run", url.Values{
		"provider": {"nope"}, "secret": {"x"},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("run: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.Contains(loc, "flash=") {
		t.Fatalf("loc=%s", loc)
	}
	// preview renders in place: the token must never land in a URL
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	resp = e.do("POST", "/import/preview", url.Values{
		"provider": {"toggl"}, "secret": {"badtoken"},
	}, nil)
	if resp.StatusCode != 200 || strings.Contains(resp.Header.Get("Location"), "badtoken") {
		t.Fatalf("preview: %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()
}

// Clockify: entries live under the key's user; running ones are skipped.
func TestClockifyUserEntries(t *testing.T) {
	var paths []string
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/user":
			fmt.Fprint(w, `{"id":"u1","activeWorkspace":"ws1"}`)
		default:
			fmt.Fprint(w, `[{"id":"e1","description":"design","timeInterval":{"start":"2026-09-01T09:00:00Z","end":"2026-09-01T10:00:00Z"}},
				{"id":"e2","description":"live","timeInterval":{"start":"2026-09-01T11:00:00Z","end":""}}]`)
		}
	})
	got, err := fetchClockifyEntries("key", "", time.Now().AddDate(0, 0, -7), time.Now())
	if err != nil || len(got) != 1 || got[0].ExtID != "clockify:e1" {
		t.Fatalf("entries %+v err %v", got, err)
	}
	if len(paths) < 2 || paths[1] != "/api/v1/workspaces/ws1/user/u1/time-entries" {
		t.Errorf("paths %v", paths)
	}
}

// Harvest: every page, inclusive "to", real clock times, no running entries.
func TestHarvestPagesAndTimes(t *testing.T) {
	var firstTo string
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			firstTo = r.URL.Query().Get("to")
			fmt.Fprint(w, `{"time_entries":[{"id":1,"notes":"a","hours":1.5,"spent_date":"2026-09-01","started_time":"8:00am"},
				{"id":2,"notes":"run","hours":1,"spent_date":"2026-09-01","is_running":true}],
				"links":{"next":"https://api.harvestapp.com/v2/time_entries?page=2"}}`)
			return
		}
		fmt.Fprint(w, `{"time_entries":[{"id":3,"notes":"b","hours":2,"spent_date":"2026-09-02"}],"links":{"next":null}}`)
	})
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	got, err := fetchHarvestEntries("t", "acc", from, from.AddDate(0, 0, 2))
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

func TestPWAAssets(t *testing.T) {
	e := newAPIEnv(t)
	for _, path := range []string{"/static/manifest.webmanifest", "/static/sw.js", "/static/icon128.png"} {
		resp := e.do("GET", path, nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// login page links the manifest + registers the SW
	resp := e.do("GET", "/login", nil, nil)
	page := readBody(t, resp)
	if !strings.Contains(page, "manifest.webmanifest") {
		t.Error("login missing manifest link")
	}
	if !strings.Contains(page, "serviceWorker") {
		t.Error("login missing SW registration")
	}
}
