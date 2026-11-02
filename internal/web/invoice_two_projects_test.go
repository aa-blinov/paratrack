package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Two clients, two projects at different rates, two weeks. Each invoice takes
// only its own project's approved time, and a second invoice cannot take hours
// that an earlier invoice already billed.
func TestInvoicesEachBillOnlyTheirOwnProjectsApprovedTime(t *testing.T) {
	e := newAPIEnv(t)
	e.register("two-projects@x.test")
	readBody(t, e.do("POST", "/api/team/calendar", url.Values{"timezone": {"UTC"}, "week_start": {"mon"}}, nil))
	readBody(t, e.do("POST", "/api/team/modules", url.Values{"preset": {"studio"}}, nil))

	for _, p := range []struct{ name, slug, rate, activity string }{
		{"Alpha", "alpha", "5000", "Дизайн"},
		{"Beta", "beta", "3000", "Разработка"},
	} {
		readBody(t, e.do("POST", "/projects/new", url.Values{"name": {p.name}}, nil))
		readBody(t, e.do("POST", "/projects/"+p.slug+"/rate", url.Values{"slug": {p.slug}, "rate": {p.rate}, "billable": {"1"}}, nil))
	}
	// Week 1 is Alpha's design hour, week 2 is Beta's two development hours.
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Дизайн"}, "start": {"2020-03-04T10:00"}, "end": {"2020-03-04T11:00"}, "note": {"alpha-1"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Разработка"}, "start": {"2020-03-11T10:00"}, "end": {"2020-03-11T12:00"}, "note": {"beta-1"}}, nil))

	projectID := func(slug string) string {
		var id int64
		if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE slug = ?`, slug).Scan(&id); err != nil {
			t.Fatalf("project %s: %v", slug, err)
		}
		return strconv.FormatInt(id, 10)
	}
	activityOf := func(note string) string {
		var id int64
		if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT activity_id FROM sessions WHERE note = ?`, note).Scan(&id); err != nil {
			t.Fatalf("activity of %s: %v", note, err)
		}
		return strconv.FormatInt(id, 10)
	}
	readBody(t, e.do("POST", "/api/activities/"+activityOf("alpha-1")+"/project", url.Values{"project_id": {projectID("alpha")}}, nil))
	readBody(t, e.do("POST", "/api/activities/"+activityOf("beta-1")+"/project", url.Values{"project_id": {projectID("beta")}}, nil))

	readBody(t, e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil))
	readBody(t, e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-09"}, "end": {"2020-03-15"}}, nil))

	readBody(t, e.do("POST", "/invoices", url.Values{"client": {"Client A"}, "start": {"2020-03-02"}, "end": {"2020-03-08"}, "project_id": {projectID("alpha")}}, nil))
	readBody(t, e.do("POST", "/invoices", url.Values{"client": {"Client B"}, "start": {"2020-03-09"}, "end": {"2020-03-15"}, "project_id": {projectID("beta")}}, nil))

	// Alpha's hour is already billed, so a second invoice for the same week and
	// project must not create another document with that time.
	readBody(t, e.do("POST", "/invoices", url.Values{"client": {"Client C"}, "start": {"2020-03-02"}, "end": {"2020-03-08"}, "project_id": {projectID("alpha")}}, nil))

	var count int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("invoices = %d, want 2: the repeat invoice for billed hours must be refused", count)
	}

	// Money is printed with a no-break space between the thousands.
	list := strings.NewReplacer(" ", " ", " ", " ").Replace(readBody(t, e.do("GET", "/invoices", nil, nil)))
	for _, want := range []string{"Client A", "5 000,00", "Client B", "6 000,00"} {
		if !strings.Contains(list, want) {
			t.Fatalf("invoice list lacks %q", want)
		}
	}
	if strings.Contains(list, "Client C") {
		t.Fatal("the refused invoice for billed hours appears in the list")
	}
}
