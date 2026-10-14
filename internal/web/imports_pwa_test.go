package web

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/importing"
	"github.com/aa-blinov/paratrack/internal/importproviders"
)

func TestImportPageAndRun(t *testing.T) {
	e := newAPIEnv(t)
	e.register("import@x.test")
	// page renders
	resp := e.do("GET", "/import", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("import page: %d %s", resp.StatusCode, readBody(t, resp))
	}
	body := readBody(t, resp)
	if !reactData[importPage](t, body).ImportReact {
		t.Fatal("import page did not bootstrap its React screen")
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
	client := fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	importer, err := importproviders.New(client, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	importService, err := importing.New(e.db, importer, e.db, e.srv.services.AuditLog, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	e.srv.services.Imports = importService
	resp = e.do("POST", "/import/preview", url.Values{
		"provider": {"toggl"}, "secret": {"badtoken"},
	}, nil)
	if resp.StatusCode != 200 || strings.Contains(resp.Header.Get("Location"), "badtoken") {
		t.Fatalf("preview: %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()
}

// Clockify: entries live under the key's user; running ones are skipped.
func TestPWAAssets(t *testing.T) {
	e := newAPIEnv(t)
	for _, path := range []string{"/static/manifest.webmanifest", "/static/sw.js", "/static/icon128.png"} {
		resp := e.do("GET", path, nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// The React bundle carries the manifest link and the service worker registration.
	resp := e.do("GET", "/login", nil, nil)
	page := readBody(t, resp)
	if !strings.Contains(page, "manifest.webmanifest") {
		t.Error("login missing manifest link")
	}
	if !strings.Contains(page, "ui/app.js") {
		t.Error("login missing the React app module")
	}
	resp = e.do("GET", "/static/ui/app.js", nil, nil)
	bundle := readBody(t, resp)
	if resp.StatusCode != 200 || !strings.Contains(bundle, "/sw.js") {
		t.Error("React bundle missing service worker registration")
	}
}
