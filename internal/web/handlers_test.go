package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDashboardBootstrapsReactFromEmbeddedAssets(t *testing.T) {
	srv, token := newTestServer(t)
	handler := srv.routes()
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	page := request("/")
	if page.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", page.Code)
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", "/static/ui/app.js?v="} {
		if !strings.Contains(page.Body.String(), marker) {
			t.Errorf("dashboard HTML missing %q", marker)
		}
	}
	const importMapOpen = `<script type="importmap">`
	const importMapClose = `</script>`
	start := strings.Index(page.Body.String(), importMapOpen)
	if start < 0 {
		t.Fatal("dashboard HTML is missing the frontend import map")
	}
	start += len(importMapOpen)
	end := strings.Index(page.Body.String()[start:], importMapClose)
	if end < 0 {
		t.Fatal("dashboard HTML has an unterminated frontend import map")
	}
	var importMap map[string]map[string]string
	if err := json.Unmarshal([]byte(page.Body.String()[start:start+end]), &importMap); err != nil {
		snippet := page.Body.String()[start : start+min(end, 100)]
		t.Fatalf("dashboard frontend import map is invalid JSON: %v; starts with %q", err, snippet)
	}

	data := request("/api/dashboard")
	if data.Code != http.StatusOK || !strings.Contains(data.Body.String(), `"data"`) {
		t.Fatalf("GET /api/dashboard status=%d body=%q", data.Code, data.Body.String())
	}
	var payload struct {
		Data struct {
			Mods      map[string]bool `json:"Mods"`
			Widgets   map[string]bool `json:"Widgets"`
			CSRFToken string          `json:"CSRFToken"`
			CanManage bool            `json:"CanManage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode GET /api/dashboard: %v", err)
	}
	if len(payload.Data.Mods) == 0 || payload.Data.Widgets["backfill"] != true || payload.Data.CSRFToken == "" {
		t.Fatalf("GET /api/dashboard omitted page context: %#v", payload.Data)
	}

	asset := request("/static/ui/app.js")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") || asset.Body.Len() < 100_000 {
		t.Fatalf("React bundle status=%d; embedded dashboard bundle missing", asset.Code)
	}
	styles := request("/static/ui/app.css")
	if styles.Code != http.StatusOK || !strings.Contains(styles.Header().Get("Content-Type"), "text/css") || styles.Body.Len() < 1_000 {
		t.Fatalf("React stylesheet status=%d; embedded dashboard styles missing", styles.Code)
	}
}
