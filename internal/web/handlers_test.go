package web

import (
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/testutil"
)

// TestNewServerSmoke verifies New() wires up successfully against an
// throwaway Postgres schema — templates parse, routes register, DB migrates.
// Heavier behavioural tests live in colors_test.go and the e2e suite.
func TestNewServerSmoke(t *testing.T) {
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	srv, err := newServerForTest(d, "127.0.0.1:0")
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
