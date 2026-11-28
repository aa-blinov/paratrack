package web

import (
	"encoding/json"
	"net/url"
	"testing"
)

// A goal can be a limit ("no more than an hour on social media"): it is
// saved as such and the progress says so, so the page and the push treat
// crossing it as a warning.
func TestGoalCanBeALimit(t *testing.T) {
	e := newAPIEnv(t)
	e.register("goal-limit@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"solo"}, "from": {"welcome"}}, nil).Body.Close()
	resp := e.do("POST", "/api/goals", url.Values{"activity": {"Соцсети"}, "period": {"daily"}, "minutes": {"60"}, "kind": {"max"}}, nil)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("save a limit: %d", resp.StatusCode)
	}
	body := readBody(t, e.do("GET", "/api/goals/progress", nil, map[string]string{"Accept": "application/json"}))
	var progress struct {
		Progress []struct {
			Goal struct {
				Kind          string `json:"kind"`
				TargetMinutes int    `json:"target_minutes"`
			} `json:"goal"`
		} `json:"progress"`
	}
	if err := json.Unmarshal([]byte(body), &progress); err != nil {
		t.Fatalf("progress: %v (%s)", err, body)
	}
	if len(progress.Progress) != 1 || progress.Progress[0].Goal.Kind != "max" || progress.Progress[0].Goal.TargetMinutes != 60 {
		t.Fatalf("progress: %+v", progress.Progress)
	}
}
