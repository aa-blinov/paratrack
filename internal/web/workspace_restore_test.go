package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// postRaw sends a raw body with the cookies and the CSRF header the way a browser
// page does, and keeps the cookies the answer sets.
func postRaw(t *testing.T, e *apiEnv, path, contentType, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", e.ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", e.jar["paratrack_csrf"])
	var parts []string
	for k, v := range e.jar {
		parts = append(parts, k+"="+v)
	}
	req.Header.Set("Cookie", strings.Join(parts, "; "))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	for _, c := range resp.Cookies() {
		if c.Value == "" || c.MaxAge < 0 {
			delete(e.jar, c.Name)
		} else {
			e.jar[c.Name] = c.Value
		}
	}
	return resp
}

// A downloaded export restores as a new workspace the caller owns, becomes the
// current one, and shows its projects. A file that is not an export is refused
// with a reason, and a file naming someone with no account is refused with that
// person's email, and neither writes a workspace.
func TestWorkspaceRestoreMakesTheDownloadAWorkspaceAndSwitchesToIt(t *testing.T) {
	e := newAPIEnv(t)
	e.register("restore-web@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Moved work"}}, nil))
	oldTeam := e.jar["paratrack_team"]
	file := readBody(t, e.do("GET", "/export/workspace.json", nil, nil))

	notAFile := postRaw(t, e, "/api/workspace/restore", "application/json", `{"hello":"world"}`)
	readBody(t, notAFile)
	if notAFile.StatusCode != http.StatusBadRequest {
		t.Fatalf("a file that is not an export: status %d, want 400", notAFile.StatusCode)
	}

	ghost := strings.ReplaceAll(file, "restore-web@x.test", "ghost@nowhere.test")
	missing := postRaw(t, e, "/api/workspace/restore", "application/json", ghost)
	var missingOut apiErrorResponse
	if err := json.Unmarshal([]byte(readBody(t, missing)), &missingOut); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if missing.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(missingOut.Error, "ghost@nowhere.test") {
		t.Fatalf("a file naming an unknown person: status %d, %q; want 422 naming the person", missing.StatusCode, missingOut.Error)
	}

	restored := postRaw(t, e, "/api/workspace/restore", "application/json", file)
	var out workspaceRestoredResponse
	if err := json.Unmarshal([]byte(readBody(t, restored)), &out); err != nil {
		t.Fatalf("decode the restore answer: %v", err)
	}
	if restored.StatusCode != http.StatusOK || out.TeamID == 0 || strconv.FormatInt(out.TeamID, 10) == oldTeam {
		t.Fatalf("restore: status %d, answer %+v (old team %s)", restored.StatusCode, out, oldTeam)
	}
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	found := false
	for _, project := range page.Projects {
		if project.Name == "Moved work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("after the restore the current workspace lists %+v, want the restored project", page.Projects)
	}
}
