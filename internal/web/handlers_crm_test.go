package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// postJSON sends a JSON body the way the React screens do: the session cookie and
// the CSRF header.
func (e *apiEnv) postJSON(path string, payload any) *http.Response {
	e.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+path, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range e.jar {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	req.Header.Set("X-CSRF-Token", e.jar["paratrack_csrf"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp
}

// assertFieldError checks a refused input: the status and the field the screen
// has to mark.
func assertFieldError(t *testing.T, resp *http.Response, status int, field string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d", resp.StatusCode, status)
	}
	var out apiFieldErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Field != field || out.Error == "" {
		t.Fatalf("error = %+v, want the field %q", out, field)
	}
}

func TestCRMAndCalendarJSONRoutesWriteAndNameTheRefusedField(t *testing.T) {
	e := newAPIEnv(t)
	e.register("crm-json@x.test")
	clientID := insertTestClient(t, e, "Василёк")

	assertFieldError(t, e.postJSON("/api/crm/deals", map[string]any{"title": " "}), http.StatusBadRequest, "deal title")

	created := e.postJSON("/api/crm/deals", map[string]any{"title": "Лендинг", "client_id": clientID, "amount_cents": 99000})
	var deal crmDealView
	decodeBody(t, created, &deal)
	if created.StatusCode != http.StatusOK || deal.ID == 0 || deal.Stage != "new" || deal.ClientID != clientID {
		t.Fatalf("create deal: status %d, deal %+v", created.StatusCode, deal)
	}

	list := e.do("GET", "/api/crm/deals", nil, nil)
	var listed crmDealsResponse
	decodeBody(t, list, &listed)
	if len(listed.Deals) != 1 || len(listed.Pipeline.Stages) != 5 {
		t.Fatalf("list deals = %+v", listed)
	}

	stagePath := fmt.Sprintf("/api/crm/deals/%d/stage", deal.ID)
	assertFieldError(t, e.postJSON(stagePath, map[string]any{"stage": "lost"}), http.StatusBadRequest, "lost_reason")
	assertFieldError(t, e.postJSON(stagePath, map[string]any{"stage": "closed"}), http.StatusBadRequest, "stage")

	start := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	assertFieldError(t, e.postJSON("/api/calendar/events", map[string]any{
		"title": "Созвон", "starts_at": start, "ends_at": start.Add(-time.Hour),
	}), http.StatusBadRequest, "ends_at")

	event := e.postJSON("/api/calendar/events", map[string]any{
		"title": "Созвон", "starts_at": start, "ends_at": start.Add(time.Hour), "shared": true,
	})
	var created2 calendarEventView
	decodeBody(t, event, &created2)
	if event.StatusCode != http.StatusOK || created2.ID == 0 || !created2.Shared {
		t.Fatalf("create event: status %d, event %+v", event.StatusCode, created2)
	}

	window := url.Values{"from": {"2026-10-25"}, "to": {"2026-10-26"}}
	agenda := e.do("GET", "/api/calendar/events?"+window.Encode(), nil, nil)
	var listedEvents calendarEventsResponse
	decodeBody(t, agenda, &listedEvents)
	if len(listedEvents.Events) != 1 {
		t.Fatalf("agenda = %+v", listedEvents)
	}

	tooLong := url.Values{"from": {"2026-10-26"}, "to": {"2028-10-26"}}
	resp := e.do("GET", "/api/calendar/events?"+tooLong.Encode(), nil, nil)
	assertFieldError(t, resp, http.StatusBadRequest, "from and to")
}

// A workspace in Vladivostok reads its days and times in that zone: an event at 08:30
// on the 27th is on the 27th there, even though it is still the 26th in UTC. An event
// that crosses midnight is listed on each day it touches.
func TestCalendarDaysAndTimesFollowTheWorkspaceZone(t *testing.T) {
	e := newAPIEnv(t)
	e.register("cal-zone@x.test")
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE teams SET timezone = 'Asia/Vladivostok' WHERE id = (SELECT id FROM teams ORDER BY id LIMIT 1)`); err != nil {
		t.Fatalf("set the workspace zone: %v", err)
	}

	morning := e.postJSON("/api/calendar/events", map[string]any{"title": "Утро", "starts_at": "2026-10-27T08:30", "ends_at": "2026-10-27T09:30"})
	var created calendarEventView
	decodeBody(t, morning, &created)
	if morning.StatusCode != http.StatusOK || !strings.HasPrefix(created.StartsAt.Format(time.RFC3339), "2026-10-27T08:30:00+10:00") {
		t.Fatalf("create in the workspace zone: status %d, starts %v", morning.StatusCode, created.StartsAt)
	}
	night := e.postJSON("/api/calendar/events", map[string]any{"title": "Ночь", "starts_at": "2026-10-26T23:00", "ends_at": "2026-10-27T01:00"})
	if night.StatusCode != http.StatusOK {
		t.Fatalf("create the overnight event: status %d", night.StatusCode)
	}

	titles := func(day string) []string {
		t.Helper()
		var out calendarEventsResponse
		decodeBody(t, e.do("GET", "/api/calendar/events?from="+day+"&to="+day, nil, nil), &out)
		var names []string
		for _, ev := range out.Events {
			names = append(names, ev.Title)
		}
		return names
	}
	if got := titles("2026-10-26"); !slices.Equal(got, []string{"Ночь"}) {
		t.Fatalf("the 26th in the workspace zone = %v, want only the overnight event", got)
	}
	if got := titles("2026-10-27"); !slices.Equal(got, []string{"Ночь", "Утро"}) {
		t.Fatalf("the 27th in the workspace zone = %v, want both", got)
	}
}

// The calendar page carries the workspace's first day of the week, so its week starts
// where the timesheet's does.
func TestCalendarPageCarriesTheWorkspaceWeekStart(t *testing.T) {
	e := newAPIEnv(t)
	e.register("cal-week@x.test")
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE teams SET week_start = 'sun' WHERE id = (SELECT id FROM teams ORDER BY id LIMIT 1)`); err != nil {
		t.Fatalf("set the week start: %v", err)
	}
	page := readBody(t, e.do("GET", "/calendar", nil, nil))
	if !strings.Contains(page, `"WeekStartsSunday":true`) {
		t.Fatal("the calendar page does not say the week starts on Sunday")
	}
}
