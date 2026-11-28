package web

import (
	"net/url"
	"testing"
)

// "Only this" on a name the workspace has not seen starts it, as the CLI
// does: the git hook switches to new branches through this route.
func TestFocusStartsAnActivityItHasNotSeen(t *testing.T) {
	e := newAPIEnv(t)
	e.register("focus-new@x.test")
	resp := e.do("POST", "/api/focus/"+url.PathEscape("PT-123-login"), nil, nil)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("focus on a new activity: %d", resp.StatusCode)
	}
	var running int
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sessions s JOIN activities a ON a.id = s.activity_id WHERE a.name = 'PT-123-login' AND s.end_at IS NULL`).Scan(&running); err != nil {
		t.Fatal(err)
	}
	if running != 1 {
		t.Fatalf("the new activity is not running: %d sessions", running)
	}
}
