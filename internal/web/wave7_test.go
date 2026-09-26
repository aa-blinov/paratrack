package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/catalog"
)

func TestCatalog(t *testing.T) {
	// marketplace has both live and coming-soon entries
	items := catalog.Integrations()
	live, soon := 0, 0
	for _, it := range items {
		if it.Available {
			live++
		} else {
			soon++
		}
	}
	if live < 8 {
		t.Fatalf("live=%d, want >= 8", live)
	}
	if soon == 0 {
		t.Fatal("no coming-soon entries")
	}
}

func TestReportTemplates(t *testing.T) {
	tpls := catalog.ReportTemplates()
	if len(tpls) < 5 {
		t.Fatalf("templates=%d", len(tpls))
	}
	ids := map[string]bool{}
	for _, tpl := range tpls {
		if ids[tpl.ID] {
			t.Fatalf("dup id %s", tpl.ID)
		}
		ids[tpl.ID] = true
	}
	for _, want := range []string{"by-project", "by-activity", "by-day", "billable", "utilization"} {
		if !ids[want] {
			t.Errorf("missing template %s", want)
		}
	}
}

func TestReportRunAndCSV(t *testing.T) {
	e := newAPIEnv(t)
	e.register("rep@x.test")
	// seed 2h
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"consulting"},
		"start":    {"yesterday 09:00"},
		"end":      {"yesterday 11:00"},
	}, map[string]string{"HX-Request": "true"})
	resp.Body.Close()

	// HTML report
	resp = e.do("GET", "/reports/run?id=by-activity&from=2026-09-20&to=2026-09-27", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("report: %d %s", resp.StatusCode, readBody(t, resp))
	}
	page := readBody(t, resp)
	if !strings.Contains(page, string(i18n.T(i18n.Default, "rep.by-activity.name"))) {
		t.Fatalf("missing title")
	}
	if !strings.Contains(page, "consulting") {
		t.Fatalf("missing row")
	}
	// CSV
	resp = e.do("GET", "/reports/run?id=by-activity&from=2026-09-20&to=2026-09-27&format=csv", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("csv: %d", resp.StatusCode)
	}
	csv := readBody(t, resp)
	if !strings.HasPrefix(csv, "key,hours,seconds") {
		t.Fatalf("csv header: %q", csv[:40])
	}
	if !strings.Contains(csv, "consulting") {
		t.Fatalf("csv missing row: %s", csv)
	}
	// gallery
	resp = e.do("GET", "/reports", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("gallery: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// marketplace
	resp = e.do("GET", "/integrations/marketplace", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("marketplace: %d %s", resp.StatusCode, readBody(t, resp))
	}
	m := readBody(t, resp)
	if !strings.Contains(m, "GitHub") || !strings.Contains(m, string(i18n.T(i18n.Default, "mkt.monday.blurb"))) {
		t.Fatalf("marketplace content: %s", m[200:500])
	}
}


