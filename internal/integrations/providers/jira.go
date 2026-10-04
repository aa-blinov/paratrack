package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var jiraKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// fetchJiraIssues lists unresolved issues via Jira Cloud REST v3
// /search/jql (the old /search was removed, 410). Pages by nextPageToken
// until isLast. fields must be named: the default is id only.
//
// secret = "email:api-token". target = [site] + project key or JQL,
// e.g. "https://acme.atlassian.net PROJ"; Config.JiraSite is the default.
// https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/
func (c *Client) fetchJiraIssues(ctx context.Context, secret, target string) ([]extItem, error) {
	site := c.config.JiraSite
	if f := strings.Fields(target); len(f) > 0 && strings.Contains(f[0], "://") {
		if !strings.HasPrefix(strings.ToLower(f[0]), "https://") {
			return nil, ErrInvalidProviderSite
		}
		site, target = f[0], strings.TrimSpace(strings.TrimPrefix(target, f[0]))
	}
	if site == "" {
		return nil, fmt.Errorf("jira: put the site first in the target, e.g. https://acme.atlassian.net PROJ")
	}
	var err error
	site, err = validateProviderSite(site)
	if err != nil {
		return nil, fmt.Errorf("jira: %w", err)
	}
	// "email:api-token" is Jira Cloud (Basic auth, /rest/api/3/search/jql,
	// nextPageToken). A bare token is a Data Center / Server PAT: Bearer
	// auth on /rest/api/2/search, paged by startAt.
	email, apiToken, cloud := strings.Cut(secret, ":")
	if !cloud {
		return c.fetchJiraDC(ctx, site, secret, jiraJQL(target))
	}
	jql := jiraJQL(target)
	var out []extItem
	token := ""
	for len(out) < maxImportTasks {
		u := site + "/rest/api/3/search/jql?jql=" + url.QueryEscape(jql) + "&maxResults=100&fields=summary,status"
		if token != "" {
			u += "&nextPageToken=" + url.QueryEscape(token)
		}
		var raw struct {
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  struct {
						Name string `json:"name"`
					} `json:"status"`
				} `json:"fields"`
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
			IsLast        bool   `json:"isLast"`
		}
		mk := func() (*http.Request, error) {
			req, err := http.NewRequest("GET", u, nil)
			if err == nil {
				req.Header.Set("Accept", "application/json")
				req.SetBasicAuth(email, apiToken)
			}
			return req, err
		}
		if _, err := c.getJSON(ctx, "jira", mk, &raw); err != nil {
			return nil, err
		}
		for _, i := range raw.Issues {
			out = append(out, extItem{ID: "jira-" + i.Key, Title: i.Key + " " + i.Fields.Summary,
				URL: site + "/browse/" + i.Key, Status: strings.ToLower(i.Fields.Status.Name)})
		}
		if raw.IsLast || raw.NextPageToken == "" {
			break
		}
		token = raw.NextPageToken
	}
	return out, nil
}

func jiraJQL(target string) string {
	switch {
	case target == "":
		return "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	case jiraKey.MatchString(target):
		return "project = " + target + " AND resolution = Unresolved ORDER BY updated DESC"
	}
	return target
}

// fetchJiraDC reads Jira Data Center / Server: GET /rest/api/2/search with
// a PAT, paged by startAt until startAt+len >= total.
// https://developer.atlassian.com/server/jira/platform/rest/v10000/api-group-search/
func (c *Client) fetchJiraDC(ctx context.Context, site, pat, jql string) ([]extItem, error) {
	var out []extItem
	for start := 0; len(out) < maxImportTasks; {
		u := fmt.Sprintf("%s/rest/api/2/search?jql=%s&startAt=%d&maxResults=100&fields=summary,status",
			site, url.QueryEscape(jql), start)
		var raw struct {
			StartAt int `json:"startAt"`
			Total   int `json:"total"`
			Issues  []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  struct {
						Name string `json:"name"`
					} `json:"status"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if _, err := c.getJSON(ctx, "jira", newReq("GET", u, "", "Authorization", "Bearer "+pat, "Accept", "application/json"), &raw); err != nil {
			return nil, err
		}
		for _, i := range raw.Issues {
			out = append(out, extItem{ID: "jira-" + i.Key, Title: i.Key + " " + i.Fields.Summary,
				URL: site + "/browse/" + i.Key, Status: strings.ToLower(i.Fields.Status.Name)})
		}
		start += len(raw.Issues)
		if len(raw.Issues) == 0 || start >= raw.Total {
			break
		}
	}
	return out, nil
}
