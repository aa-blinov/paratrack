package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The global search answers a text with groups of links, and a text too short to search
// with no groups at all (UI-01).
func TestGlobalSearchAnswersWithGroupsOfLinks(t *testing.T) {
	e := newAPIEnv(t)
	e.register("global-search@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Поисковый проект"}}, nil))

	found := e.do("GET", "/api/search?q="+url.QueryEscape("поисков"), nil, map[string]string{"Accept": "application/json"})
	body := readBody(t, found)
	if found.StatusCode != http.StatusOK || !strings.Contains(body, "Поисковый проект") || !strings.Contains(body, "/projects/") {
		t.Fatalf("the search missed the project: status %d, body %s", found.StatusCode, body)
	}

	short := e.do("GET", "/api/search?q=п", nil, map[string]string{"Accept": "application/json"})
	shortBody := readBody(t, short)
	if !strings.Contains(strings.Join(strings.Fields(shortBody), ""), `"groups":[]`) {
		t.Fatalf("a one-letter search answered groups: %s", shortBody)
	}
}
