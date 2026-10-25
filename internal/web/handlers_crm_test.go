package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
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

// The pipeline belongs to the workspace. A member reads it and is refused to change
// it; a manager adds a stage, renames it, moves it, and the deals in it count there.
// A stage with a deal is refused on delete with the field named.
func TestCRMStagesAreManagedByManagersAndCountInThePipeline(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("crm-stage-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("crm-stage-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"crm-stage-member@x.test": member}, "crm-stage-member@x.test")

	var listed crmStagesResponse
	decodeBody(t, member.do("GET", "/api/crm/stages", nil, nil), &listed)
	if len(listed.Stages) != 5 || listed.Stages[0].Key != "new" || listed.Stages[4].Kind != "lost" {
		t.Fatalf("a member reads the stages = %+v, want the five defaults", listed.Stages)
	}
	if resp := member.postJSON("/api/crm/stages", map[string]any{"name": "Чужой этап"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member adds a stage: status %d, want 403", resp.StatusCode)
	}

	var created crmStageConfigView
	decodeBody(t, owner.postJSON("/api/crm/stages", map[string]any{"name": "Созвон"}), &created)
	if created.Key != "созвон" || created.Position != 6 || created.Kind != "open" {
		t.Fatalf("created stage = %+v, want key созвон at position 6, open", created)
	}
	stagePath := "/api/crm/stages/" + url.PathEscape(created.Key)
	var renamed crmStageConfigView
	decodeBody(t, owner.postJSON(stagePath, map[string]any{"name": "Звонок и встреча"}), &renamed)
	if renamed.Key != "созвон" || renamed.Name != "Звонок и встреча" {
		t.Fatalf("renamed stage = %+v, the key must stay", renamed)
	}

	var deal crmDealView
	decodeBody(t, owner.postJSON("/api/crm/deals", map[string]any{"title": "Встреча", "stage": created.Key}), &deal)
	if deal.Stage != created.Key {
		t.Fatalf("deal in the new stage = %+v", deal)
	}
	var moved crmStageConfigView
	decodeBody(t, owner.postJSON(stagePath, map[string]any{"position": 1}), &moved)
	if moved.Position != 1 {
		t.Fatalf("moved stage = %+v, want position 1", moved)
	}

	var board crmDealsResponse
	decodeBody(t, owner.do("GET", "/api/crm/deals", nil, nil), &board)
	if len(board.Pipeline.Stages) != 6 || board.Pipeline.Stages[0].Stage != created.Key ||
		board.Pipeline.Stages[0].Name != "Звонок и встреча" || board.Pipeline.Stages[0].Count != 1 {
		t.Fatalf("pipeline after the change = %+v", board.Pipeline.Stages)
	}

	assertFieldError(t, owner.postJSON(stagePath+"/delete", map[string]any{}), http.StatusBadRequest, "stage")
	if resp := member.postJSON(stagePath+"/delete", map[string]any{}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member deletes a stage: status %d, want 403", resp.StatusCode)
	}
}

// A won deal makes its project once. The project carries the deal's name and client, its
// amount as the budget and no billable rate, and the deal keeps the project. An open deal, a
// second call and a deal without a client are refused, each on the field that must change.
func TestCRMWonDealMakesItsProjectOnce(t *testing.T) {
	e := newAPIEnv(t)
	e.register("crm-project@x.test")
	clientID := insertTestClient(t, e, "Ромашка")

	var deal crmDealView
	decodeBody(t, e.postJSON("/api/crm/deals", map[string]any{"title": "Сайт Ромашки", "client_id": clientID, "amount_cents": 250000, "currency": "RUB"}), &deal)
	projectPath := fmt.Sprintf("/api/crm/deals/%d/project", deal.ID)
	assertFieldError(t, e.postJSON(projectPath, map[string]any{}), http.StatusBadRequest, "stage")

	var won crmDealView
	decodeBody(t, e.postJSON(fmt.Sprintf("/api/crm/deals/%d/stage", deal.ID), map[string]any{"stage": "won"}), &won)
	if won.Stage != "won" || won.ProjectID != 0 {
		t.Fatalf("won deal = %+v, want no project yet", won)
	}

	var made crmDealView
	decodeBody(t, e.postJSON(projectPath, map[string]any{}), &made)
	if made.ProjectID == 0 || made.ID != deal.ID {
		t.Fatalf("project of the won deal: %+v", made)
	}
	var name, currency string
	var client int64
	var budget, rate sql.NullInt64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT name, client_id, budget_cents, billable_rate_cents, currency FROM projects WHERE id = ?`, made.ProjectID).
		Scan(&name, &client, &budget, &rate, &currency); err != nil {
		t.Fatalf("load the project: %v", err)
	}
	if name != "Сайт Ромашки" || client != clientID || !budget.Valid || budget.Int64 != 250000 || rate.Valid || currency != "RUB" {
		t.Fatalf("project = name %q, client %d, budget %v, rate %v, currency %q", name, client, budget, rate, currency)
	}

	assertFieldError(t, e.postJSON(projectPath, map[string]any{}), http.StatusBadRequest, "project")

	var bare crmDealView
	decodeBody(t, e.postJSON("/api/crm/deals", map[string]any{"title": "Без клиента", "stage": "won"}), &bare)
	assertFieldError(t, e.postJSON(fmt.Sprintf("/api/crm/deals/%d/project", bare.ID), map[string]any{}), http.StatusBadRequest, "client")
}

// memberDealAnswer reads a deal answer a member got. It has no amount and no currency, and
// says so with amount_visible false.
func memberDealAnswer(t *testing.T, resp *http.Response) crmDealView {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member's deal answer: status %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	assertMemberDealKeys(t, body)
	var deal crmDealView
	if err := json.Unmarshal([]byte(body), &deal); err != nil {
		t.Fatal(err)
	}
	return deal
}

// assertMemberDealKeys checks the keys of one deal object a member read: no amount_cents and no
// currency, and amount_visible set to false. The answers are indented, so the keys are read, not
// matched as text.
func assertMemberDealKeys(t *testing.T, object string) {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(object), &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"amount_cents", "currency"} {
		if _, ok := keys[key]; ok {
			t.Fatalf("a member's deal carries %s: %s", key, object)
		}
	}
	if string(keys["amount_visible"]) != "false" {
		t.Fatalf("a member's deal does not say amount_visible is false: %s", object)
	}
}

// Amounts are a manager's. A member reads every deal of the workspace with its title, client,
// stage and owner, and never its amount or currency, in the list or in a stage move.
func TestCRMDealAmountsShowToManagersOnly(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "crm-read")
	clientID := insertClientInTeam(t, owner, mcpStudioID(t, owner), "Лютик")

	var managed crmDealView
	decodeBody(t, owner.postJSON("/api/crm/deals", map[string]any{"title": "Сайт Лютика", "client_id": clientID, "amount_cents": 250000, "currency": "RUB"}), &managed)
	if !managed.AmountVisible || managed.AmountCents != 250000 || managed.Currency != "RUB" {
		t.Fatalf("manager's create answer = %+v", managed)
	}
	var listed crmDealsResponse
	decodeBody(t, owner.do("GET", "/api/crm/deals", nil, nil), &listed)
	if len(listed.Deals) != 1 || !listed.Deals[0].AmountVisible || listed.Deals[0].AmountCents != 250000 || listed.Deals[0].Currency != "RUB" {
		t.Fatalf("manager's list = %+v", listed.Deals)
	}

	raw := readBody(t, member.do("GET", "/api/crm/deals", nil, nil))
	var seen crmDealsResponse
	if err := json.Unmarshal([]byte(raw), &seen); err != nil {
		t.Fatal(err)
	}
	var rows struct {
		Deals []json.RawMessage `json:"deals"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatal(err)
	}
	if len(seen.Deals) != 1 || len(rows.Deals) != 1 || seen.Deals[0].Title != "Сайт Лютика" || seen.Deals[0].ClientID != clientID {
		t.Fatalf("member's list = %s, want the manager's deal", raw)
	}
	assertMemberDealKeys(t, string(rows.Deals[0]))

	own := memberDealAnswer(t, member.postJSON("/api/crm/deals", map[string]any{"title": "Лендинг Лютика"}))
	owner.postJSON(fmt.Sprintf("/api/crm/deals/%d", own.ID), map[string]any{"amount_cents": 99000})
	moved := memberDealAnswer(t, member.postJSON(fmt.Sprintf("/api/crm/deals/%d/stage", own.ID), map[string]any{"stage": "working"}))
	if moved.Stage != "working" {
		t.Fatalf("member's stage move = %+v", moved)
	}
}

// A member cannot set an amount or a currency. A create or an update that names one is refused on
// "amount"; a member's zero is no amount given, so it leaves the amount a manager set as it is.
func TestCRMMemberCannotSetAnAmount(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "crm-write")

	assertFieldError(t, member.postJSON("/api/crm/deals", map[string]any{"title": "Дорого", "amount_cents": 250000}), http.StatusBadRequest, "amount")
	assertFieldError(t, member.postJSON("/api/crm/deals", map[string]any{"title": "Дорого", "currency": "RUB"}), http.StatusBadRequest, "amount")

	own := memberDealAnswer(t, member.postJSON("/api/crm/deals", map[string]any{"title": "Лендинг"}))
	path := fmt.Sprintf("/api/crm/deals/%d", own.ID)
	owner.postJSON(path, map[string]any{"amount_cents": 99000})

	assertFieldError(t, member.postJSON(path, map[string]any{"amount_cents": 250000}), http.StatusBadRequest, "amount")
	renamed := memberDealAnswer(t, member.postJSON(path, map[string]any{"title": "Лендинг 2", "amount_cents": 0}))
	if renamed.Title != "Лендинг 2" {
		t.Fatalf("member's title change = %+v", renamed)
	}
	var stored int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT amount_cents FROM crm_deals WHERE id = ?`, own.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 99000 {
		t.Fatalf("a member's zero changed the manager's amount: %d", stored)
	}
}

// A member who owns a won deal is refused by the projects rule, and the answer says why.
// Nothing is written for the refusal.
func TestCRMMemberIsRefusedTheProjectWithTheReason(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "crm-project")
	clientID := insertClientInTeam(t, owner, mcpStudioID(t, owner), "Лютик")

	var deal crmDealView
	decodeBody(t, member.postJSON("/api/crm/deals", map[string]any{"title": "Лендинг Лютика", "client_id": clientID, "stage": "won"}), &deal)
	resp := member.postJSON(fmt.Sprintf("/api/crm/deals/%d/project", deal.ID), map[string]any{})
	status := resp.StatusCode
	var refused apiErrorResponse
	decodeBody(t, resp, &refused)
	if status != http.StatusForbidden || refused.Error != i18n.T(i18n.Ru, "crm.projectManagerOnly") {
		t.Fatalf("member makes the project: status %d, answer %+v", status, refused)
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM projects WHERE name = ?`, "Лендинг Лютика"); n != 0 {
		t.Fatalf("a refused member left %d projects behind", n)
	}
}
