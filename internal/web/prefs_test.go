package web

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Personal preferences change only this person's view.
func TestPreferences(t *testing.T) {
	e := newAPIEnv(t)
	e.register("prefs@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}, "rate": {"3000"}}, nil))
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Лютик"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:30"}, "project_id": {"1"}}, htmx))

	if page := readBody(t, e.do("GET", "/settings/preferences", nil, nil)); !strings.Contains(page, `name="duration"`) {
		t.Fatal("no preferences form")
	}
	resp := e.do("POST", "/api/me/preferences", url.Values{
		"duration": {"clock"}, "week_start": {"sun"}, "tz": {"Asia/Novosibirsk"},
		"sections_all": {"graph", "goals"}, "sections": {"goals"},
		"tabs":            {"goals", "stats", "graph"}, // graph is hidden, so it can't be a tab
		"widgets":         {"unbilled", "goals", "backfill"},
		"default_project": {"2"},
	}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("save: %q", loc)
	}
	dash := readBody(t, e.do("GET", "/", nil, nil))
	if !strings.Contains(dash, `data-durfmt="clock"`) {
		t.Error("duration format not on the page")
	}
	if strings.Contains(dash, `"graph":true`) {
		t.Error("a section I hid is still in my menu")
	}
	if !strings.Contains(dash, `"goals":true`) {
		t.Error("goals vanished though only graph was hidden")
	}
	if !strings.Contains(dash, `id="recent" class="card bg-base-100 border border-base-300 mt-4" hidden`) {
		t.Error("recent block not hidden")
	}
	if !strings.Contains(dash, `data-default="2"`) {
		t.Error("default project not preselected")
	}
	// Workspace routes still work: hiding is personal, not a lock.
	if r := e.do("GET", "/graph", nil, nil); r.StatusCode != 200 {
		t.Errorf("/graph after personal hide: %d", r.StatusCode)
	}

	req := prefReq(t, "/timesheet", Prefs{WeekStart: "sun"})
	sun := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC) // a Sunday
	if got := startOfWeek(req, sun); got.Day() != 27 {
		t.Errorf("sunday week starts %v", got)
	}
	if got := startOfWeek(prefReq(t, "/", Prefs{}), sun); got.Day() != 21 {
		t.Errorf("monday week starts %v", got)
	}
	tabs := e.srv.tabsFor(prefReq(t, "/", Prefs{Tabs: []string{"goals", "stats"}}), map[string]bool{"goals": true})
	if len(tabs) != 4 || tabs[0].Key != "goals" || tabs[1].Key != "stats" {
		t.Errorf("tabs: %+v", tabs)
	}
}

func prefReq(t *testing.T, path string, p Prefs) *http.Request {
	t.Helper()
	r, _ := http.NewRequest("GET", path, nil)
	return r.WithContext(withPrefs(r.Context(), p))
}

// Workspace billing rules: rounding per line, number prefix, logo.
func TestBillingRules(t *testing.T) {
	e := newAPIEnv(t)
	e.register("bill@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}, "rate": {"3000"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 10:20"}, "project_id": {"1"}}, htmx))

	resp := e.do("POST", "/api/team/billing", url.Values{"round_minutes": {"15"}, "round_mode": {"up"}, "invoice_prefix": {"СЧ"}}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("billing save: %q", loc)
	}
	resp = e.do("POST", "/api/team/billing", url.Values{"round_minutes": {"7"}, "invoice_prefix": {"СЧ"}}, nil)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "bad_request") {
		t.Error("7 minutes accepted")
	}
	resp = e.do("POST", "/api/team/billing", url.Values{"round_minutes": {"0"}, "invoice_prefix": {"<b>"}}, nil)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "bad_request") {
		t.Error("prefix with markup accepted")
	}

	// Logo: multipart with the token as a field, like the browser sends it.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf_token", e.jar["paratrack_csrf"])
	fw, _ := mw.CreateFormFile("logo", "logo.png")
	fw.Write(pngBuf.Bytes())
	mw.Close()
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/team/logo", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range e.jar {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	lr, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	lr.Body.Close()
	if loc := lr.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("logo upload: %d %q", lr.StatusCode, loc)
	}

	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	resp = e.do("POST", "/invoices", url.Values{"project_id": {"1"}, "client": {"ООО «Ромашка»"}, "start": {day}, "end": {day}}, nil)
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	page := readBody(t, e.do("GET", loc, nil, nil))
	if !strings.Contains(page, "СЧ-") {
		t.Error("invoice number lacks the prefix")
	}
	if !strings.Contains(page, "1 500,00") && !strings.Contains(page, "1 500,00") {
		t.Error("20 min rounded up to 15-min steps should bill 30 min = 1 500,00")
	}
	if !strings.Contains(page, `src="data:image/png;base64,`) {
		t.Error("logo not on the invoice")
	}
	pdf := readBody(t, e.do("GET", loc+"/pdf", nil, nil))
	if !strings.HasPrefix(pdf, "%PDF") || !strings.Contains(pdf, "/Image") {
		t.Error("PDF has no logo image")
	}
}

// Project page: "all time" is the whole history, and /projects shows a
// member only their own hours.
func TestProjectTotalsAllTimeAndScoped(t *testing.T) {
	e := newAPIEnv(t)
	e.register("proj@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}}, nil))
	old := time.Now().AddDate(0, -3, 0).Format("2006-01-02")
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {old + " 10:00"}, "end": {old + " 13:00"}, "project_id": {"1"}}, htmx))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 11:00"}, "project_id": {"1"}}, htmx))
	proj, _ := e.db.GetProjectByID(t.Context(), 1)
	page := readBody(t, e.do("GET", "/projects/"+proj.Slug, nil, nil))
	if !strings.Contains(page, "4\u00a0ч") {
		t.Errorf("all-time total should be 4 h (3 h three months ago + 1 h yesterday)")
	}
	list := readBody(t, e.do("GET", "/projects", nil, nil))
	if !strings.Contains(list, "1 ч") {
		t.Errorf("30-day column should be 1 h")
	}
}

// /api/v1/sessions pages through everything once, newest first, running
// timers included, ties on the same start kept apart by id.
func TestAPIv1SessionsPagination(t *testing.T) {
	e := newAPIEnv(t)
	e.register("page@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	for i := 0; i < 5; i++ {
		readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"задача " + strconv.Itoa(i)}, "start": {"вчера 10:00"}, "end": {"вчера 11:00"}}, htmx))
	}
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"ранняя"}, "start": {"вчера 08:00"}, "end": {"вчера 09:00"}}, htmx))
	readBody(t, e.do("POST", "/api/v1/sessions", url.Values{"activity": {"идёт"}}, nil))

	type page struct {
		Sessions []struct {
			ID    int64  `json:"id"`
			Start string `json:"start"`
		} `json:"sessions"`
		Next string `json:"next_cursor"`
	}
	seen := map[int64]bool{}
	var starts []string
	cursor, pages := "", 0
	for {
		q := "/api/v1/sessions?limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		var p page
		if err := json.Unmarshal([]byte(readBody(t, e.do("GET", q, nil, nil))), &p); err != nil {
			t.Fatal(err)
		}
		pages++
		for _, s := range p.Sessions {
			if seen[s.ID] {
				t.Fatalf("session %d came twice", s.ID)
			}
			seen[s.ID] = true
			starts = append(starts, s.Start)
		}
		if p.Next == "" || pages > 10 {
			break
		}
		cursor = p.Next
	}
	if len(seen) != 7 || pages != 4 {
		t.Fatalf("got %d sessions in %d pages, want 7 in 4", len(seen), pages)
	}
	for i := 1; i < len(starts); i++ {
		if starts[i] > starts[i-1] {
			t.Fatalf("not newest first: %v", starts)
		}
	}
	if r := e.do("GET", "/api/v1/sessions?cursor=bogus", nil, nil); r.StatusCode != 400 {
		t.Errorf("bad cursor: %d, want 400", r.StatusCode)
	}
}
