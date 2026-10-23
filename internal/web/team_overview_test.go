package web

import "testing"

// The statistics page carries the manager's one-screen week: the people's load, with the
// team's plan and fact by project (PLAN-05).
func TestManagerStatsCarriesTheTeamOverview(t *testing.T) {
	e := newAPIEnv(t)
	e.register("team-overview@x.test")
	page := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil)))
	if page.Overview == nil {
		t.Fatal("the manager's statistics page carries no team overview")
	}
	if len(page.Overview.People) == 0 || page.Overview.DayNorm == "" {
		t.Fatalf("the overview holds %+v, want the people and the daily norm", page.Overview)
	}
	if page.Overview.Projects == nil || page.Overview.People == nil {
		t.Fatal("an empty overview list came back as null")
	}
}
