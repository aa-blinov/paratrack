package web

import (
	"net/url"
	"testing"
	"time"
)

// An empty /stats period has to stay reachable. "Сегодня" is the default
// period because it is the same window the dashboard totals, so yesterday's
// hours live outside it — an empty page that names no other period reads as
// "nothing was ever tracked", and the reader has no way to tell otherwise.
func TestStatsEmptyPeriodOffersPeriodsThatHoldTime(t *testing.T) {
	e := newAPIEnv(t)
	e.register("empty-period@x.test")

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"reading"}, "start": {yesterday + " 09:00"}, "end": {yesterday + " 09:45"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("backfill yesterday: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	data := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil)))
	if len(data.Sessions) != 0 {
		t.Fatalf("default period is today and holds %d sessions, want none", len(data.Sessions))
	}
	offered := map[string]statsPeriodOption{}
	for _, option := range data.Elsewhere {
		offered[option.Label] = option
	}
	if option, ok := offered["yesterday"]; !ok || option.Count != 1 || option.Total == "" {
		t.Fatalf("empty today must offer yesterday's one session, got %+v", data.Elsewhere)
	}
	if _, ok := offered["today"]; ok {
		t.Errorf("the empty period itself must not be offered as elsewhere: %+v", data.Elsewhere)
	}

	// The period the empty state names has to lead to the session it counted.
	linked := reactData[statsData](t, readBody(t, e.do("GET", "/stats?period=yesterday", nil, nil)))
	if len(linked.Sessions) != 1 {
		t.Fatalf("yesterday view shows %d sessions, want 1", len(linked.Sessions))
	}
	if len(linked.Elsewhere) != 0 {
		t.Errorf("a period that holds sessions needs no elsewhere hint, got %+v", linked.Elsewhere)
	}
}
