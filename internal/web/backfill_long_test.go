package web

import (
	"net/url"
	"testing"
)

// A session longer than ten hours is not saved by the form until the person
// confirms it (TIME-18). The first save answers with the confirmation header and
// writes nothing; the confirmed save goes through.
func TestBackfillAsksBeforeSavingALongSession(t *testing.T) {
	e := newAPIEnv(t)
	e.register("backfill-long@x.test")
	form := url.Values{"activity": {"Long day"}, "start": {"yesterday 08:00"}, "end": {"yesterday 20:00"}}

	first := e.do("POST", "/api/sessions/backfill", form, nil)
	body := readBody(t, first)
	if first.StatusCode != 409 || first.Header.Get("X-Backfill-Confirm") != "long" {
		t.Fatalf("a 12 h session saved without asking: status %d, confirm %q, body %q",
			first.StatusCode, first.Header.Get("X-Backfill-Confirm"), body)
	}
	if first.Header.Get("X-Backfill-Saved") == "true" {
		t.Fatal("the unconfirmed save reports itself as saved")
	}

	confirmed := url.Values{}
	for key, values := range form {
		confirmed[key] = values
	}
	confirmed.Set("confirm_long", "1")
	second := e.do("POST", "/api/sessions/backfill", confirmed, nil)
	readBody(t, second)
	if second.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("the confirmed save did not go through: status %d", second.StatusCode)
	}
}

// Shorter sessions are saved at once, as before.
func TestBackfillSavesAShortSessionWithoutAsking(t *testing.T) {
	e := newAPIEnv(t)
	e.register("backfill-short@x.test")
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Short day"}, "start": {"yesterday 09:00"}, "end": {"yesterday 11:00"},
	}, nil)
	readBody(t, resp)
	if resp.Header.Get("X-Backfill-Confirm") != "" || resp.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("a 2 h session was held back: status %d, confirm %q, saved %q",
			resp.StatusCode, resp.Header.Get("X-Backfill-Confirm"), resp.Header.Get("X-Backfill-Saved"))
	}
}
