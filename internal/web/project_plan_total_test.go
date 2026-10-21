package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// projectPlanPage is the part of the project page the plan tests read.
type projectPlanPage struct {
	PlannedLabel string
	TrackedLabel string
	People       []projectPersonRow
}

// The project page shows the plan once. The total is the sum of the people
// rows, and only a manager sees the tracked column and its total.
func TestProjectPlanTotalIsTheSumOfThePeopleRows(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("plan-total-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("plan-total-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"plan-total-member@x.test": member}, "plan-total-member@x.test")
	for _, name := range []string{"Totals", "Empty"} {
		readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {name}}, nil))
	}

	userID := func(email string) int64 {
		var id int64
		if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	var totalsID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = ?`, "Totals").Scan(&totalsID); err != nil {
		t.Fatal(err)
	}
	ownerID := userID("plan-total-owner@x.test")
	memberID := userID("plan-total-member@x.test")

	today := time.Now().Format("2006-01-02")
	plan := func(user int64, minutes string) {
		r := owner.do("POST", "/api/schedule/cell", url.Values{
			"user_id": {strconv.FormatInt(user, 10)}, "project_id": {strconv.FormatInt(totalsID, 10)},
			"date": {today}, "minutes": {minutes},
		}, map[string]string{"Accept": "application/json"})
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("plan %s minutes: status %d, want 200", minutes, r.StatusCode)
		}
	}
	plan(ownerID, "600")
	plan(memberID, "300")

	page := func(who *apiEnv, slug string) projectPlanPage {
		return reactData[projectPlanPage](t, readBody(t, who.do("GET", "/projects/"+slug, nil, nil)))
	}
	hours := func(h int) string { return fmtDurF(i18n.Ru, "hm", h*60*60) }

	ownerView := page(owner, "totals")
	if ownerView.PlannedLabel != hours(15) {
		t.Fatalf("total plan = %q, want %q: the rows are 10 h and 5 h", ownerView.PlannedLabel, hours(15))
	}
	if len(ownerView.People) != 2 {
		t.Fatalf("people rows = %d, want 2", len(ownerView.People))
	}
	for _, row := range ownerView.People {
		if row.Tracked == "" {
			t.Fatalf("a manager sees no tracked figure for %q", row.Name)
		}
	}
	if ownerView.TrackedLabel == "" {
		t.Fatal("a manager does not see the tracked total")
	}

	memberView := page(member, "totals")
	if memberView.PlannedLabel != hours(15) {
		t.Fatalf("member's total plan = %q, want %q", memberView.PlannedLabel, hours(15))
	}
	if memberView.TrackedLabel != "" {
		t.Fatalf("a member sees the tracked total %q", memberView.TrackedLabel)
	}
	for _, row := range memberView.People {
		if row.Tracked != "" {
			t.Fatalf("a member sees the tracked figure %q for %q", row.Tracked, row.Name)
		}
	}

	// An unplanned project has no rows; the page hides its total row with them.
	if empty := page(owner, "empty"); len(empty.People) != 0 {
		t.Fatalf("an unplanned project lists %d people", len(empty.People))
	}
}
