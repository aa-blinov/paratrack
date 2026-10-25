package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// With no OAuth client configured the sync is off: the page still renders, a connect is
// refused with a notice, and a member with no connection gets a clear 409.

func TestCalendarSyncSettingsPageRendersWithTheSyncOff(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-sync-page@x.test")
	resp := e.do("GET", "/settings/calendar-sync", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"CalendarSyncReact":true`) {
		t.Fatalf("settings page: status %d, body lacks the calendar sync screen", resp.StatusCode)
	}
}

func TestCalendarSyncConnectWithoutProviderRedirectsWithNotice(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-sync-connect@x.test")
	resp := e.do("POST", "/settings/calendar-sync/google/connect", url.Values{}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "calsync.notice.disabled") {
		t.Fatalf("connect: status %d to %q, want a notice that sync is off", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestCalendarSyncCallbackWithoutConsentIsRefused(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-sync-callback@x.test")
	resp := e.do("GET", "/settings/calendar-sync/google/callback?state=forged&code=stolen", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "calsync.notice.disabled") {
		t.Fatalf("callback: status %d to %q, want the refusal notice", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestCalendarSyncRunRefusesMemberWithoutConnection(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-sync-run@x.test")
	resp := e.postJSON("/api/calendar/sync", map[string]any{})
	defer resp.Body.Close()
	var out apiErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict || out.Error == "" {
		t.Fatalf("sync without a connection: status %d, error %q; want 409 with a message", resp.StatusCode, out.Error)
	}
}

func TestCalendarSyncEventPushSwitchNeedsAConnection(t *testing.T) {
	e := newAPIEnv(t)
	e.register("calendar-sync-push@x.test")
	resp := e.postJSON("/api/calendar/events", map[string]any{
		"title": "Демо", "starts_at": "2026-10-26T10:00", "ends_at": "2026-10-26T11:00", "sync_push": true,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("push switch without a connection: status %d, want 409", resp.StatusCode)
	}
	plain := e.postJSON("/api/calendar/events", map[string]any{
		"title": "Без отправки", "starts_at": "2026-10-26T10:00", "ends_at": "2026-10-26T11:00",
	})
	defer plain.Body.Close()
	if plain.StatusCode != http.StatusOK {
		t.Fatalf("event without the switch: status %d", plain.StatusCode)
	}
}
