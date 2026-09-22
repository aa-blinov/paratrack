package web

import (
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/db"
)

// TestNewServerSmoke verifies New() wires up successfully against an
// in-memory SQLite DB — templates parse, routes register, DB migrates.
// Heavier behavioural tests live in colors_test.go and the e2e suite.
func TestNewServerSmoke(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open :memory: db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	srv, err := New(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	// The dashboard should respond 200 to GET / even with no data.
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("GET /: status %d", resp.StatusCode)
	}
	resp.Body.Close()
}
