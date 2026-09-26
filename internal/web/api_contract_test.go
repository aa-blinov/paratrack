package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// apiEnv is a logged-in server + cookie jar helper for API tests.
type apiEnv struct {
	t   *testing.T
	ts  *httptest.Server
	srv *Server
	jar map[string]string // cookie name → value
}

func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	srv, err := New(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return &apiEnv{t: t, ts: ts, srv: srv, jar: map[string]string{}}
}

func (e *apiEnv) do(method, path string, form url.Values, hdr map[string]string) *http.Response {
	e.t.Helper()
	// Seed the CSRF cookie on first use so mutating calls can echo it.
	if _, ok := e.jar["paratrack_csrf"]; !ok {
		req, _ := http.NewRequest("GET", e.ts.URL+"/login", nil)
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if resp, err := client.Do(req); err == nil {
			for _, c := range resp.Cookies() {
				e.jar[c.Name] = c.Value
			}
			resp.Body.Close()
		}
	}
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, e.ts.URL+path, body)
	if err != nil {
		e.t.Fatalf("request: %v", err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	// Attach cookies + CSRF header (double-submit).
	var parts []string
	for k, v := range e.jar {
		parts = append(parts, k+"="+v)
	}
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	if tok := e.jar["paratrack_csrf"]; tok != "" && req.Header.Get("X-CSRF-Token") == "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	// Don't follow redirects — we want the 303 + Set-Cookie as-is.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("do %s %s: %v", method, path, err)
	}
	// Store set-cookie (last wins, good enough for tests).
	for _, c := range resp.Cookies() {
		if c.Value == "" || c.MaxAge < 0 {
			delete(e.jar, c.Name)
		} else {
			e.jar[c.Name] = c.Value
		}
	}
	return resp
}

func (e *apiEnv) register(email string) {
	e.t.Helper()
	resp := e.do("POST", "/api/register", url.Values{
		"name": {"U"}, "email": {email}, "password": {"longenoughpw"},
	}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("register: status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

func TestAPISessionLifecyclePauseExcludedFromTracked(t *testing.T) {
	e := newAPIEnv(t)
	e.register("life@x.test")

	// Start.
	resp := e.do("POST", "/api/start", url.Values{"activity": {"reading"}}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "reading") {
		t.Fatalf("start fragment missing activity: %q", body[:min(80, len(body))])
	}

	// Find the session id from /api/active fragment.
	resp = e.do("GET", "/api/active", nil, nil)
	active := readBody(t, resp)
	id := ""
	for _, part := range strings.Split(active, `sessions/`) {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			id = strings.SplitN(part, "/", 2)[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no session id in active fragment: %q", active[:min(120, len(active))])
	}

	// Pause → resume → stop, then duration_seconds in CSV must be > 0
	// and reflect tracked time (not left at 0 like the old stop path).
	resp = e.do("POST", "/api/sessions/"+id+"/pause", nil, nil)
	resp.Body.Close()
	time.Sleep(1100 * time.Millisecond)
	resp = e.do("POST", "/api/sessions/"+id+"/resume", nil, nil)
	resp.Body.Close()
	time.Sleep(200 * time.Millisecond)
	resp = e.do("POST", "/api/sessions/"+id+"/stop", nil, nil)
	resp.Body.Close()

	resp = e.do("GET", "/api/reports.csv", nil, nil)
	csv := readBody(t, resp)
	// duration_seconds is the 6th column.
	found := false
	for _, line := range strings.Split(csv, "\n") {
		if !strings.Contains(line, "reading") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			t.Fatalf("csv line too short: %q", line)
		}
		// Parse quoted CSV-ish: id,activity,project,start,end,duration
		// The activity field is %q-quoted so commas inside notes don't
		// break us here — duration is the field after the RFC3339 end.
		for i, f := range fields {
			if strings.Contains(f, "T") && strings.Contains(f, "Z") && i+1 < len(fields) {
				dur := strings.TrimSpace(fields[i+1])
				if dur == "" || dur == "0" {
					t.Errorf("duration_seconds = %q, want > 0 (stop must fold elapsed into accumulated)", dur)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no reading row with timestamps in CSV:\n%s", csv)
	}
}

func TestAPIGoalsHTMXReturnsFragmentJSONKeepsShape(t *testing.T) {
	e := newAPIEnv(t)
	e.register("goals@x.test")

	// HTMX create → HTML fragment.
	resp := e.do("POST", "/api/goals", url.Values{
		"activity": {"deep work"}, "period": {"daily"}, "minutes": {"120"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("goals create hx: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("HTMX goals create returned JSON, want fragment: %q", body[:min(60, len(body))])
	}
	if !strings.Contains(body, "deep work") {
		t.Fatalf("fragment missing activity: %q", body[:min(120, len(body))])
	}

	// Delete with a space in the name (url-encoded) via HTMX.
	q := url.Values{"activity": {"deep work"}, "period": {"daily"}}.Encode()
	resp = e.do("DELETE", "/api/goals?"+q, nil, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("goals delete hx: %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	body = readBody(t, resp)
	if strings.Contains(body, "deep work") {
		t.Fatalf("goal still listed after delete: %q", body[:min(80, len(body))])
	}

	// JSON shape still works for tooling.
	resp = e.do("POST", "/api/goals", url.Values{
		"activity": {"reading"}, "period": {"weekly"}, "minutes": {"300"},
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("goals create json: %d", resp.StatusCode)
	}
	body = readBody(t, resp)
	var g map[string]any
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatalf("goals json create: %v body=%s", err, body)
	}
	if g["period"] != "weekly" {
		t.Errorf("period = %v, want weekly", g["period"])
	}

	// Unknown goal → 404, not 500.
	resp = e.do("DELETE", "/api/goals?activity=nope&period=daily", nil, nil)
	if resp.StatusCode != 404 {
		t.Errorf("delete missing goal: status %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAPITagsHTMXAndTeamScope(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tags-a@x.test")

	resp := e.do("POST", "/api/tags", url.Values{"name": {"deep-work"}}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("tag create hx: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("HTMX tag create returned JSON: %q", body[:min(40, len(body))])
	}

	// Grab the tag id from the fragment (hx-delete="/api/tags?id=N").
	id := ""
	for _, part := range strings.Split(body, "/api/tags?id=") {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			id = strings.SplitN(part, `"`, 2)[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no tag id in fragment: %q", body[:min(160, len(body))])
	}

	// Second user must NOT be able to delete this tag.
	e2 := newAPIEnvSharedDB(t, e)
	e2.register("tags-b@x.test")
	resp = e2.do("DELETE", "/api/tags?id="+id, nil, nil)
	if resp.StatusCode != 404 {
		t.Errorf("cross-team tag delete: status %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Owner can.
	resp = e.do("DELETE", "/api/tags?id="+id, nil, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("owner tag delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// newAPIEnvSharedDB builds a second server over the SAME db so we can
// exercise multi-tenant isolation without exporting the handle.
func newAPIEnvSharedDB(t *testing.T, base *apiEnv) *apiEnv {
	t.Helper()
	// Recover the *db.DB from the first server via its teams service is
	// not exported — instead hit the same routes with a fresh auth
	// session on a cloned server sharing the file. For :memory: this
	// won't work, so we use the same srv and just a new cookie jar.
	return &apiEnv{t: t, ts: base.ts, srv: base.srv, jar: map[string]string{}}
}

func TestAPICSVUsesProjectNameAndTrackedDuration(t *testing.T) {
	e := newAPIEnv(t)
	e.register("csv@x.test")

	// Create a project, start an activity bound to it, stop it.
	resp := e.do("POST", "/projects/new", url.Values{
		"name": {"EORA RAG"}, "color": {"#7c3aed"},
	}, nil)
	resp.Body.Close()

	resp = e.do("POST", "/api/start", url.Values{
		"activity": {"rag-eval"}, "project_id": {"1"}, "note": {"with, comma"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("start with project: %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	// Stop every active session.
	resp = e.do("GET", "/api/active", nil, nil)
	active := readBody(t, resp)
	for _, part := range strings.Split(active, `sessions/`) {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			id := strings.SplitN(part, "/", 2)[0]
			r := e.do("POST", "/api/sessions/"+id+"/stop", nil, nil)
			r.Body.Close()
		}
	}

	resp = e.do("GET", "/api/reports.csv", nil, nil)
	csv := readBody(t, resp)
	if !strings.Contains(csv, "EORA RAG") {
		t.Errorf("CSV should carry project NAME not slug:\n%s", csv)
	}
	if strings.Contains(csv, "eora-rag") {
		t.Errorf("CSV still has slug in project column:\n%s", csv)
	}
	if !strings.Contains(csv, "duration_seconds") {
		t.Errorf("CSV missing header:\n%s", csv)
	}
}

func TestAPIStartRejectsDuplicateActive(t *testing.T) {
	e := newAPIEnv(t)
	e.register("dup@x.test")
	resp := e.do("POST", "/api/start", url.Values{"activity": {"work"}}, nil)
	resp.Body.Close()
	resp = e.do("POST", "/api/start", url.Values{"activity": {"work"}}, nil)
	if resp.StatusCode != 409 {
		t.Errorf("duplicate start: status %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAPIUpdateSessionRecomputesEndFromDuration(t *testing.T) {
	e := newAPIEnv(t)
	e.register("edit@x.test")
	resp := e.do("POST", "/api/start", url.Values{"activity": {"writing"}}, map[string]string{"HX-Request": "true"})
	body := readBody(t, resp)
	id := ""
	for _, part := range strings.Split(body, `sessions/`) {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			id = strings.SplitN(part, "/", 2)[0]
			break
		}
	}
	if id == "" {
		t.Fatalf("no id: %q", body[:min(80, len(body))])
	}
	// Close it first so we can edit a closed session's duration.
	resp = e.do("POST", "/api/sessions/"+id+"/stop", nil, nil)
	resp.Body.Close()

	start := time.Now().Add(-2 * time.Hour).Format("2006-01-02T15:04")
	resp = e.do("PATCH", "/api/sessions/"+id, url.Values{
		"start_at": {start}, "duration": {"2h 30m"}, "note": {"revised"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("patch: %d %s", resp.StatusCode, readBody(t, resp))
	}
	row := readBody(t, resp)
	if !strings.Contains(row, fmtDurL(i18n.Default, 9000)) {
		t.Errorf("row should show 2h 30m: %q", row[:min(200, len(row))])
	}
	if !strings.Contains(row, "revised") {
		t.Errorf("row should show note: %q", row[:min(200, len(row))])
	}
}
