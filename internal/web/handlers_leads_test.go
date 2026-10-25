package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// createIntakeKey makes the workspace's intake key the way the app does and returns it.
func createIntakeKey(t *testing.T, e *apiEnv) string {
	t.Helper()
	resp := e.do("POST", "/api/crm/intake-key", nil, nil)
	var out struct {
		Active bool   `json:"active"`
		Key    string `json:"key"`
	}
	decodeBody(t, resp, &out)
	if resp.StatusCode != http.StatusOK || !out.Active || !strings.HasPrefix(out.Key, "lk_") {
		t.Fatalf("make the intake key: status %d, %+v", resp.StatusCode, out)
	}
	return out.Key
}

// postIntake sends an enquiry the way the company website does: no cookie, the key in the
// header, and JSON.
func postIntake(t *testing.T, e *apiEnv, headers map[string]string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/api/leads/intake", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const intakeBody = `{"company":"Лотос","contact_name":"Анна","phone":"+7 900 000-00-00","email":"anna@lotos.test","source":"website","message":"Нужен сайт"}`

func TestLeadIntakeAcceptsTheKeyAndAnswersOnlyTheID(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-intake@x.test")
	key := createIntakeKey(t, e)

	resp := postIntake(t, e, map[string]string{"X-Intake-Key": key}, intakeBody)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("intake status = %d, body %s", resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("answer %q: %v", raw, err)
	}
	if len(out) != 1 || out["id"] == nil {
		t.Fatalf("answer = %v, want only the id", out)
	}

	list := e.do("GET", "/api/crm/leads", nil, nil)
	var leads crmLeadsResponse
	decodeBody(t, list, &leads)
	if len(leads.Leads) != 1 {
		t.Fatalf("leads = %+v, want the enquiry", leads)
	}
	lead := leads.Leads[0]
	if lead.Status != "new" || lead.OwnerID != 0 || lead.Company != "Лотос" || lead.Source != "website" || lead.Message != "Нужен сайт" {
		t.Fatalf("lead = %+v, want status new, no owner and the website's fields", lead)
	}
}

func TestLeadIntakeRefusesWrongMissingAndCookieRequests(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-refuse@x.test")
	key := createIntakeKey(t, e)

	for name, headers := range map[string]map[string]string{
		"a wrong key": {"X-Intake-Key": "lk_" + strings.Repeat("0", 64)},
		"no key":      {},
		"a bearer":    {"Authorization": "Bearer " + apiTokenOf(t, e, "agent")},
	} {
		resp := postIntake(t, e, headers, intakeBody)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", name, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// A signed-in browser sends its cookies to the route, and still gets no lead without the key.
	resp := e.do("POST", "/api/leads/intake", nil, map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session cookie without the key: status %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// A missing or empty body with no key is still 401, not a field error.
	resp = postIntake(t, e, nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty request without a key: status %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	list := e.do("GET", "/api/crm/leads", nil, nil)
	var leads crmLeadsResponse
	decodeBody(t, list, &leads)
	if len(leads.Leads) != 0 {
		t.Fatalf("a refused request wrote %d leads", len(leads.Leads))
	}

	// The key itself still works after the refusals.
	if resp := postIntake(t, e, map[string]string{"X-Intake-Key": key}, intakeBody); resp.StatusCode != http.StatusCreated {
		t.Fatalf("the right key after refusals: status %d", resp.StatusCode)
	}
}

func TestLeadIntakeNamesTheFieldThatIsWrong(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-fields@x.test")
	key := createIntakeKey(t, e)
	headers := map[string]string{"X-Intake-Key": key}

	assertFieldError(t, postIntake(t, e, headers, `{"source":"website","message":"Перезвоните"}`), http.StatusBadRequest, "contact")
	assertFieldError(t, postIntake(t, e, headers, `{"contact_name":"Анна","email":"не почта"}`), http.StatusBadRequest, "email")
	assertFieldError(t, postIntake(t, e, headers, `{"contact_name":"Анна","phone":"`+strings.Repeat("1", 41)+`"}`), http.StatusBadRequest, "phone")
	assertFieldError(t, postIntake(t, e, headers, `{"contact_name":"Анна","message":"`+strings.Repeat("я", 4001)+`"}`), http.StatusBadRequest, "message")
	if resp := postIntake(t, e, headers, `{"contact_name":"Анна","owner_id":1,"status":"qualified"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("an unknown field from the website: status %d, want the lead recorded", resp.StatusCode)
	}
}

func TestLeadIntakeAnswersThePreflightWithItsHeadersOnly(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-cors@x.test")
	key := createIntakeKey(t, e)

	req, err := http.NewRequest(http.MethodOptions, e.ts.URL+"/api/leads/intake", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://company.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-intake-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q, want *", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Intake-Key" {
		t.Fatalf("allow headers = %q, want only the two the form sends", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials allowed = %q, want none", got)
	}

	answer := postIntake(t, e, map[string]string{"X-Intake-Key": key, "Origin": "https://company.test"}, intakeBody)
	answer.Body.Close()
	if got := answer.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("the answer to a cross-origin enquiry is not readable: allow origin %q", got)
	}
}

func TestLeadIntakeIsBudgetedPerKey(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-budget@x.test")
	key := createIntakeKey(t, e)
	headers := map[string]string{"X-Intake-Key": key}

	for i := 0; i < 30; i++ {
		resp := postIntake(t, e, headers, `{"contact_name":"Анна"}`)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("enquiry %d: status %d, want 201 inside the budget", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp := postIntake(t, e, headers, `{"contact_name":"Анна"}`)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("enquiry 31: status %d, want 429 (%s)", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "too many requests") {
		t.Fatalf("429 body = %s", raw)
	}
}

func TestCRMIntakeKeyReplacedRevokedAndManagedByManagers(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("leads-key-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("leads-key-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"leads-key-member@x.test": member}, "leads-key-member@x.test")

	if resp := member.do("POST", "/api/crm/intake-key", nil, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member made the intake key: status %d, want 403", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	status := owner.do("GET", "/api/crm/intake-key", nil, nil)
	var before struct {
		Active bool `json:"active"`
	}
	decodeBody(t, status, &before)
	if before.Active {
		t.Fatal("a workspace with no key reads as active")
	}

	first := createIntakeKey(t, owner)
	resp := owner.do("GET", "/api/crm/intake-key", nil, nil)
	raw := readBody(t, resp)
	if strings.Contains(raw, first) || strings.Contains(raw, "\"key\"") {
		t.Fatalf("the status read shows the secret: %s", raw)
	}

	second := createIntakeKey(t, owner)
	if resp := postIntake(t, owner, map[string]string{"X-Intake-Key": first}, intakeBody); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the replaced key still takes enquiries: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := postIntake(t, owner, map[string]string{"X-Intake-Key": second}, intakeBody); resp.StatusCode != http.StatusCreated {
		t.Fatalf("the new key: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	revoke := owner.do("POST", "/api/crm/intake-key/revoke", nil, nil)
	var revoked struct {
		Active bool `json:"active"`
	}
	decodeBody(t, revoke, &revoked)
	if revoked.Active {
		t.Fatal("revoked key still reads as active")
	}
	if resp := postIntake(t, owner, map[string]string{"X-Intake-Key": second}, intakeBody); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the revoked key takes enquiries: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

func TestCRMLeadRoutesFilterConvertAndKeepTheTimeline(t *testing.T) {
	e := newAPIEnv(t)
	e.register("leads-routes@x.test")
	clientless := e.postJSON("/api/crm/leads", map[string]any{"company": "Ромашка", "email": "buy@romashka.test", "source": "referral"})
	var created crmLeadView
	decodeBody(t, clientless, &created)
	if clientless.StatusCode != http.StatusOK || created.ID == 0 || created.Status != "new" || created.OwnerID == 0 {
		t.Fatalf("create lead: status %d, %+v", clientless.StatusCode, created)
	}
	assertFieldError(t, e.postJSON("/api/crm/leads", map[string]any{"source": "referral"}), http.StatusBadRequest, "contact")

	lostPath := "/api/crm/leads/" + intToString(created.ID) + "/status"
	assertFieldError(t, e.postJSON(lostPath, map[string]any{"status": "lost"}), http.StatusBadRequest, "lost_reason")

	activity := e.postJSON("/api/crm/activities", map[string]any{"lead_id": created.ID, "kind": "call", "body": "Перезвонить в пятницу"})
	activity.Body.Close()
	if activity.StatusCode != http.StatusOK {
		t.Fatalf("log on the lead: status %d", activity.StatusCode)
	}
	timeline := e.do("GET", "/api/crm/activities?lead_id="+intToString(created.ID), nil, nil)
	var acts crmActivitiesResponse
	decodeBody(t, timeline, &acts)
	if len(acts.Activities) != 1 || acts.Activities[0].LeadID != created.ID {
		t.Fatalf("lead timeline = %+v", acts)
	}

	convert := e.postJSON("/api/crm/leads/"+intToString(created.ID)+"/convert", map[string]any{})
	var converted crmLeadView
	decodeBody(t, convert, &converted)
	if convert.StatusCode != http.StatusOK || converted.Status != "converted" || converted.ConvertedClientID == 0 || converted.ConvertedDealID == 0 {
		t.Fatalf("convert: status %d, %+v", convert.StatusCode, converted)
	}
	again := e.postJSON("/api/crm/leads/"+intToString(created.ID)+"/convert", map[string]any{})
	assertFieldError(t, again, http.StatusBadRequest, "status")

	converted2 := e.do("GET", "/api/crm/leads?status=converted", nil, nil)
	var listed crmLeadsResponse
	decodeBody(t, converted2, &listed)
	if len(listed.Leads) != 1 || listed.Leads[0].ID != created.ID {
		t.Fatalf("filter by status = %+v", listed)
	}
	if bad := e.do("GET", "/api/crm/leads?status=won", nil, nil); bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown status filter: status %d, want 400", bad.StatusCode)
	} else {
		bad.Body.Close()
	}
}
