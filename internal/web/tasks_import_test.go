package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A GitHub import that cannot start answers with a flash on the board, and the
// token is never echoed back in the answer.
func TestGitHubImportAnswersWithAFlashAndNeverEchoesTheToken(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-github@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Board import"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	if !page.CanImport {
		t.Fatal("the owner is not offered the GitHub import")
	}
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	const secret = "ghp_should_not_come_back"

	resp := e.do("POST", "/api/tasks/import/github", url.Values{
		"project_id": {projectID}, "project_url": {"https://github.com/acme/site"}, "token": {secret},
	}, nil)
	readBody(t, resp)
	location := resp.Header.Get("Location")
	if resp.StatusCode != 303 || !strings.Contains(location, "/tasks?view=table&project="+projectID) || !strings.Contains(location, "flash=") {
		t.Fatalf("a refused import redirects to %d %q, want the board with a flash", resp.StatusCode, location)
	}
	if strings.Contains(location, secret) {
		t.Fatalf("the token shows in the redirect: %q", location)
	}
}
