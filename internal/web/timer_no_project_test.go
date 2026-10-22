package web

import (
	"net/url"
	"testing"
)

// A timer started from the dashboard with no project asks once (TIME-12): the
// unconfirmed start runs nothing, the confirmed one runs. API starts are not asked.
func TestDashboardTimerWithoutProjectAsksOnce(t *testing.T) {
	e := newAPIEnv(t)
	e.register("timer-no-project@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	asked := e.do("POST", "/api/start", url.Values{"activity": {"Loose start"}, "ask_no_project": {"1"}}, jsonHeader)
	readBody(t, asked)
	if asked.StatusCode != 409 || asked.Header.Get("X-Start-Confirm") != "no-project" {
		t.Fatalf("a dashboard timer with no project started without asking: status %d, confirm %q",
			asked.StatusCode, asked.Header.Get("X-Start-Confirm"))
	}
	if ids := activeSessionIDs(t, e); len(ids) != 0 {
		t.Fatalf("the unconfirmed start left %d running timer(s)", len(ids))
	}

	confirmed := e.do("POST", "/api/start", url.Values{
		"activity": {"Loose start"}, "ask_no_project": {"1"}, "confirm_no_project": {"1"},
	}, jsonHeader)
	readBody(t, confirmed)
	if confirmed.StatusCode >= 300 || len(activeSessionIDs(t, e)) != 1 {
		t.Fatalf("the confirmed start did not run: status %d", confirmed.StatusCode)
	}

	api := e.do("POST", "/api/start", url.Values{"activity": {"API start"}}, jsonHeader)
	readBody(t, api)
	if api.StatusCode >= 300 || api.Header.Get("X-Start-Confirm") != "" {
		t.Fatalf("an API start was asked: status %d, confirm %q", api.StatusCode, api.Header.Get("X-Start-Confirm"))
	}
}
