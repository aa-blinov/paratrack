package web

import (
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
	if _, err := fetchClockifyEntries("key", "", time.Now(), time.Now()); err == nil ||
		!strings.Contains(err.Error(), "workspace id") {
		t.Fatalf("clockify: %v", err)
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
	// preview with unknown provider
	resp = e.do("POST", "/import/preview", url.Values{
		"provider": {"toggl"}, "secret": {"badtoken"},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("preview: %d", resp.StatusCode)
	}
	resp.Body.Close()
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
