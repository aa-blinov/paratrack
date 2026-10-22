package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

// The workspace's own zone and first day of the week win over a member's personal
// choice, and a workspace that set neither leaves the member's choice in force (ADM-07).
func TestWorkspaceCalendarOverridesThePersonalOne(t *testing.T) {
	personal := Prefs{TZ: "Europe/Moscow", WeekStart: "mon"}
	withTeam := func(team model.Team) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(r.Context(), prefsKey{}, personal)
		return r.WithContext(WithTeam(ctx, team))
	}

	if got := userLoc(withTeam(model.Team{TimeZone: "Asia/Tokyo", WeekStart: "sun"})).String(); got != "Asia/Tokyo" {
		t.Fatalf("the workspace zone lost to the personal one: %s", got)
	}
	if !weekStartsSunday(withTeam(model.Team{WeekStart: "sun"})) {
		t.Fatal("the workspace week starting on Sunday was ignored")
	}

	own := withTeam(model.Team{})
	if got := userLoc(own).String(); got != "Europe/Moscow" {
		t.Fatalf("a workspace with no zone hid the member's own: %s", got)
	}
	if weekStartsSunday(own) {
		t.Fatal("a workspace with no week hid the member's own Monday start")
	}
}
