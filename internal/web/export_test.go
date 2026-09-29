package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestExportExplainsCSVAndFiltersDates(t *testing.T) {
	e := newAPIEnv(t)
	e.register("export@x.test")

	resp := e.do("GET", "/export", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("export page: %d %s", resp.StatusCode, readBody(t, resp))
	}
	page := readBody(t, resp)
	for _, want := range []string{"Завершённые сеансы", "Текущие таймеры в выгрузку не входят", "paratrack.csv", `action="/api/reports.csv"`} {
		if !strings.Contains(page, want) {
			t.Errorf("export page missing %q", want)
		}
	}

	for _, session := range []struct{ activity, date string }{
		{"earlier", "2026-01-04"},
		{"chosen", "2026-01-06"},
	} {
		resp = e.do("POST", "/api/sessions/backfill", url.Values{
			"activity": {session.activity}, "start": {session.date + " 09:00"}, "end": {session.date + " 10:00"},
		}, map[string]string{"HX-Request": "true"})
		if resp.StatusCode != 200 {
			t.Fatalf("backfill %s: %d %s", session.activity, resp.StatusCode, readBody(t, resp))
		}
		resp.Body.Close()
	}

	resp = e.do("GET", "/api/reports.csv?from=2026-01-06&to=2026-01-06", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "paratrack.csv") {
		t.Fatalf("filtered CSV: status=%d disposition=%s", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
	csv := readBody(t, resp)
	if !strings.Contains(csv, "chosen") || strings.Contains(csv, "earlier") {
		t.Fatalf("date filter: %s", csv)
	}

	resp = e.do("GET", "/api/reports.csv", nil, nil)
	csv = readBody(t, resp)
	if !strings.Contains(csv, "chosen") || !strings.Contains(csv, "earlier") {
		t.Fatalf("unfiltered CSV changed: %s", csv)
	}

	resp = e.do("GET", "/api/reports.csv?from=2026-01-07&to=2026-01-06", nil, nil)
	if resp.StatusCode != 400 {
		t.Errorf("reversed date range: %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
	resp = e.do("GET", "/api/reports.csv?from=oops", nil, nil)
	if resp.StatusCode != 400 {
		t.Errorf("invalid date: %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
