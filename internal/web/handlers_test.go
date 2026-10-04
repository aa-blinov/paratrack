package web

import (
	"encoding/json"
	"io/fs"
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
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") || asset.Body.Len() < 10_000 {
		t.Fatalf("React bundle status=%d; embedded dashboard bundle missing", asset.Code)
	}
	styles := request("/static/ui/app.css")
	if styles.Code != http.StatusOK || !strings.Contains(styles.Header().Get("Content-Type"), "text/css") || styles.Body.Len() < 1_000 {
		t.Fatalf("React stylesheet status=%d; embedded dashboard styles missing", styles.Code)
	}
	chunks, err := fs.Glob(assets, "static/ui/chunks/*.js")
	if err != nil || len(chunks) < 5 {
		t.Fatalf("React screen chunks = %v, err=%v; want route and shared runtime chunks embedded", chunks, err)
	}
	for _, chunk := range chunks {
		response := request("/" + chunk)
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
			t.Errorf("GET %s status=%d content-type=%q", chunk, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

func TestProjectsListBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	handler := srv.routes()
	for _, path := range []string{"/projects", "/projects?archived=1", "/projects/new"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%q", path, response.Code, response.Body.String())
		}
		for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"ReactApp":true`} {
			if !strings.Contains(response.Body.String(), marker) {
				t.Errorf("GET %s missing React bootstrap marker %q", path, marker)
			}
		}
	}
}

func TestGoalsPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/goals", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /goals status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"GoalsReact":true`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /goals missing React bootstrap marker %q", marker)
		}
	}
}

func TestTagsPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /tags status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"ReactTags":true`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /tags missing React bootstrap marker %q", marker)
		}
	}
}

func TestGraphPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/graph?period=week", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /graph status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"GraphReact":true`, `"ChartJSON"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /graph missing React bootstrap marker %q", marker)
		}
	}
}

func TestStatsPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/stats?period=week", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /stats status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"Active":"stats"`, `"ByProject"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /stats missing React bootstrap marker %q", marker)
		}
	}
}

func TestReportsPagesBootstrapReact(t *testing.T) {
	srv, token := newTestServer(t)
	for _, tc := range []struct {
		path    string
		markers []string
	}{
		{path: "/reports", markers: []string{`"ReportsReact":true`, `"Templates"`}},
		{path: "/reports/run?id=by-project", markers: []string{`"ReportRunReact":true`, `"VM"`, `"billable":true`}},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		response := httptest.NewRecorder()
		srv.routes().ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%q", tc.path, response.Code, response.Body.String())
		}
		for _, marker := range append([]string{`id="react-page-data"`, `id="paratrack-react-root"`}, tc.markers...) {
			if !strings.Contains(response.Body.String(), marker) {
				t.Errorf("GET %s missing React bootstrap marker %q", tc.path, marker)
			}
		}
	}
}

func TestExportPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /export status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, `"ReportsEnabled"`, `"CanManage"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /export missing React bootstrap marker %q", marker)
		}
	}
}

func TestIntegrationPagesBootstrapReact(t *testing.T) {
	srv, token := newTestServer(t)
	for _, tc := range []struct {
		path   string
		marker string
	}{
		{path: "/integrations", marker: `"IntegrationsReact":true`},
		{path: "/integrations/marketplace", marker: `"MarketReact":true`},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		response := httptest.NewRecorder()
		srv.routes().ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%q", tc.path, response.Code, response.Body.String())
		}
		for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, tc.marker} {
			if !strings.Contains(response.Body.String(), marker) {
				t.Errorf("GET %s missing React bootstrap marker %q", tc.path, marker)
			}
		}
	}
}

func TestTokensPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/tokens", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /settings/tokens status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, `"TokensReact":true`, `"Tokens"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /settings/tokens missing React bootstrap marker %q", marker)
		}
	}
}

func TestProfilePageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /settings/profile status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, `"ProfileReact":true`, `"User"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /settings/profile missing React bootstrap marker %q", marker)
		}
	}
}

func TestPreferencesPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/preferences", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /settings/preferences status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, `"PrefsReact":true`, `"Zones"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /settings/preferences missing React bootstrap marker %q", marker)
		}
	}
}

func TestNotificationsPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/notifications", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /settings/notifications status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{`id="react-page-data"`, `id="paratrack-react-root"`, `"NotificationsReact":true`, `"DeviceCount"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /settings/notifications missing React bootstrap marker %q", marker)
		}
	}
}

func TestTimesheetPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/timesheet", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /timesheet status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"TimesheetReact":true`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /timesheet missing React bootstrap marker %q", marker)
		}
	}
}

func TestPayrollPageBootstrapsReact(t *testing.T) {
	srv, token := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/payroll", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /payroll status=%d body=%q", response.Code, response.Body.String())
	}
	for _, marker := range []string{"id=\"react-page-data\"", "id=\"paratrack-react-root\"", `"PayrollReact":true`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Errorf("GET /payroll missing React bootstrap marker %q", marker)
		}
	}
}
