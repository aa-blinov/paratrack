package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Task importers. Every list is read to the end, page by page, the way
// each vendor documents it (docs checked 2026-09-27; links per fetcher),
// up to maxImportTasks so a runaway cursor can't loop forever.
const maxImportTasks = 2000

// retryWaits are the pauses before retrying a 429/503; tests shorten them.
var retryWaits = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}

// apiDo sends req (rebuilt by mk on each try) and retries rate limits:
// 429 or 503, and GitHub's 403 with retry-after / x-ratelimit-remaining 0.
// It waits what the server asks (Retry-After seconds, or the reset time in
// X-RateLimit-Reset / x-ratelimit-reset as unix seconds), capped at 30 s.
// A non-2xx that isn't a rate limit comes back as an error with the body.
func apiDo(vendor string, mk func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := mk()
		if err != nil {
			return nil, err
		}
		resp, err := extClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		limited := resp.StatusCode == 429 || resp.StatusCode == 503 ||
			(resp.StatusCode == 403 && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-Ratelimit-Remaining") == "0"))
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		if !limited || attempt >= len(retryWaits) {
			return nil, fmt.Errorf("%s %d: %s", vendor, resp.StatusCode, strings.TrimSpace(string(b)))
		}
		time.Sleep(rateLimitWait(resp.Header, retryWaits[attempt]))
	}
}

func rateLimitWait(h http.Header, fallback time.Duration) time.Duration {
	wait := fallback
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s >= 0 {
		wait = time.Duration(s) * time.Second
	} else if r, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64); err == nil && r > 1e9 {
		if r > 1e12 { // ClickUp and friends send milliseconds
			r /= 1000
		}
		wait = time.Until(time.Unix(r, 0))
	}
	if wait < 0 {
		wait = 0
	}
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	return wait
}

func getJSON(vendor string, mk func() (*http.Request, error), into any) (http.Header, error) {
	resp, err := apiDo(vendor, mk)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.Header, json.NewDecoder(resp.Body).Decode(into)
}

func newReq(method, u, body string, headers ...string) func() (*http.Request, error) {
	return func() (*http.Request, error) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, u, rd)
		if err != nil {
			return nil, err
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		return req, nil
	}
}

// linkNext reads rel="next" from an RFC 8288 Link header (GitHub, GitLab).
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func linkNext(h http.Header) string {
	if m := linkNextRe.FindStringSubmatch(h.Get("Link")); m != nil {
		return m[1]
	}
	return ""
}

// fetchGitHubIssues lists open issues of "owner/repo", or the token
// owner's assigned issues across repos when repo is empty. Pull requests
// come back from this API too and are skipped. per_page max 100, pages
// via Link rel="next".
// https://docs.github.com/en/rest/issues/issues · …/using-pagination-in-the-rest-api
func fetchGitHubIssues(token, repo string) ([]extItem, error) {
	next := "https://api.github.com/issues?state=open&per_page=100"
	if parts := strings.SplitN(repo, "/", 2); len(parts) == 2 {
		next = fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=100",
			url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	}
	var out []extItem
	for next != "" && len(out) < maxImportTasks {
		var raw []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			State   string `json:"state"`
			Repo    struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PullRequest *struct{} `json:"pull_request"`
		}
		h, err := getJSON("github", newReq("GET", next, "",
			"Authorization", "Bearer "+token, "Accept", "application/vnd.github+json",
			"X-GitHub-Api-Version", "2022-11-28", "User-Agent", "paratrack"), &raw)
		if err != nil {
			return nil, err
		}
		for _, i := range raw {
			if i.PullRequest != nil {
				continue
			}
			full, name := i.Repo.FullName, i.Title
			if full != "" {
				name = full + "#" + strconv.Itoa(i.Number) + " " + i.Title
			} else {
				full = repo
			}
			// Numbers repeat across repos: the id carries the repo.
			out = append(out, extItem{ID: fmt.Sprintf("gh-%s#%d", full, i.Number), Title: name, URL: i.HTMLURL, Status: i.State})
		}
		next = linkNext(h)
	}
	return out, nil
}

// fetchTrelloCards lists open cards on a board; secret is "key:token".
// No page param: limit ≤ 1000, older cards via before=<last card id>.
// https://developer.atlassian.com/cloud/trello/guides/rest-api/nested-resources/
func fetchTrelloCards(secret, board string) ([]extItem, error) {
	key, token, ok := strings.Cut(secret, ":")
	if !ok || key == "" || token == "" {
		return nil, fmt.Errorf("trello: the secret is key:token")
	}
	const limit = 1000
	var out []extItem
	before := ""
	for len(out) < maxImportTasks {
		u := fmt.Sprintf("https://api.trello.com/1/boards/%s/cards/open?limit=%d&key=%s&token=%s",
			url.PathEscape(board), limit, url.QueryEscape(key), url.QueryEscape(token))
		if before != "" {
			u += "&before=" + url.QueryEscape(before)
		}
		var raw []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			ShortURL string `json:"shortUrl"`
			Closed   bool   `json:"closed"`
		}
		if _, err := getJSON("trello", newReq("GET", u, ""), &raw); err != nil {
			return nil, err
		}
		for _, c := range raw {
			st := "open"
			if c.Closed {
				st = "closed"
			}
			out = append(out, extItem{ID: "tr-" + c.ID, Title: c.Name, URL: c.ShortURL, Status: st})
		}
		if len(raw) < limit {
			break
		}
		before = raw[len(raw)-1].ID
	}
	return out, nil
}

var jiraKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// fetchJiraIssues lists unresolved issues via Jira Cloud REST v3
// /search/jql (the old /search was removed, 410). Pages by nextPageToken
// until isLast. fields must be named: the default is id only.
//
// secret = "email:api-token". target = [site] + project key or JQL,
// e.g. "https://acme.atlassian.net PROJ"; PARATRACK_JIRA_SITE is the default.
// https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/
func fetchJiraIssues(secret, target string) ([]extItem, error) {
	site := os.Getenv("PARATRACK_JIRA_SITE")
	if f := strings.Fields(target); len(f) > 0 && strings.HasPrefix(f[0], "https://") {
		site, target = f[0], strings.TrimSpace(strings.TrimPrefix(target, f[0]))
	}
	if site == "" {
		return nil, fmt.Errorf("jira: put the site first in the target, e.g. https://acme.atlassian.net PROJ")
	}
	site = strings.TrimRight(site, "/")
	// "email:api-token" is Jira Cloud (Basic auth, /rest/api/3/search/jql,
	// nextPageToken). A bare token is a Data Center / Server PAT: Bearer
	// auth on /rest/api/2/search, paged by startAt.
	email, apiToken, cloud := strings.Cut(secret, ":")
	if !cloud {
		return fetchJiraDC(site, secret, jiraJQL(target))
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
		if _, err := getJSON("jira", mk, &raw); err != nil {
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
func fetchJiraDC(site, pat, jql string) ([]extItem, error) {
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
		if _, err := getJSON("jira", newReq("GET", u, "", "Authorization", "Bearer "+pat, "Accept", "application/json"), &raw); err != nil {
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

// notionVersion is the data-sources API: a database can hold several data
// sources, and the old databases/{id}/query fails on those.
// https://developers.notion.com/docs/upgrade-guide-2025-09-03 · …/reference/pagination
const notionVersion = "2025-09-03"

// fetchNotionTasks reads every page of every data source of a database.
// secret = internal integration secret; target = database id (32 hex).
// No filter: a database needn't have a "Status" property.
func fetchNotionTasks(secret, dbID string) ([]extItem, error) {
	if dbID == "" {
		return nil, fmt.Errorf("notion database id is required")
	}
	dbID = strings.ReplaceAll(dbID, "-", "")
	if len(dbID) != 32 {
		return nil, fmt.Errorf("notion database id must be 32 hex chars")
	}
	formatted := dbID[0:8] + "-" + dbID[8:12] + "-" + dbID[12:16] + "-" + dbID[16:20] + "-" + dbID[20:32]
	hdr := []string{"Authorization", "Bearer " + secret, "Notion-Version", notionVersion, "Content-Type", "application/json"}
	var db struct {
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}
	if _, err := getJSON("notion", newReq("GET", "https://api.notion.com/v1/databases/"+formatted, "", hdr...), &db); err != nil {
		return nil, err
	}
	var out []extItem
	for _, src := range db.DataSources {
		cursor := ""
		for len(out) < maxImportTasks {
			body := map[string]any{"page_size": 100}
			if cursor != "" {
				body["start_cursor"] = cursor
			}
			b, _ := json.Marshal(body)
			var raw struct {
				Results []struct {
					ID         string `json:"id"`
					URL        string `json:"url"`
					InTrash    bool   `json:"in_trash"`
					Properties map[string]struct {
						Title []struct {
							PlainText string `json:"plain_text"`
						} `json:"title"`
					} `json:"properties"`
				} `json:"results"`
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			}
			if _, err := getJSON("notion", newReq("POST", "https://api.notion.com/v1/data_sources/"+src.ID+"/query", string(b), hdr...), &raw); err != nil {
				return nil, err
			}
			for _, r := range raw.Results {
				if r.InTrash {
					continue
				}
				title := "Untitled"
				for _, v := range r.Properties {
					if len(v.Title) > 0 && v.Title[0].PlainText != "" {
						title = v.Title[0].PlainText
						break
					}
				}
				out = append(out, extItem{ID: "notion-" + r.ID, Title: title, URL: r.URL, Status: "open"})
			}
			if !raw.HasMore || raw.NextCursor == "" {
				break
			}
			cursor = raw.NextCursor
		}
	}
	return out, nil
}

// fetchAsanaTasks lists incomplete tasks of a project (completed_since=now).
// limit ≤ 100; next page via next_page.offset, until next_page is null.
// https://developers.asana.com/reference/gettasksforproject · …/docs/pagination
func fetchAsanaTasks(token, projectGID string) ([]extItem, error) {
	if projectGID == "" {
		return nil, fmt.Errorf("asana project gid is required")
	}
	var out []extItem
	offset := ""
	for len(out) < maxImportTasks {
		u := fmt.Sprintf("https://app.asana.com/api/1.0/projects/%s/tasks?opt_fields=name,completed,permalink_url&completed_since=now&limit=100",
			url.PathEscape(projectGID))
		if offset != "" {
			u += "&offset=" + url.QueryEscape(offset)
		}
		var raw struct {
			Data []struct {
				GID       string `json:"gid"`
				Name      string `json:"name"`
				Completed bool   `json:"completed"`
				Permalink string `json:"permalink_url"`
			} `json:"data"`
			NextPage *struct {
				Offset string `json:"offset"`
			} `json:"next_page"`
		}
		if _, err := getJSON("asana", newReq("GET", u, "", "Authorization", "Bearer "+token, "Accept", "application/json"), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Data {
			if !t.Completed {
				out = append(out, extItem{ID: "asana-" + t.GID, Title: t.Name, URL: t.Permalink, Status: "open"})
			}
		}
		if raw.NextPage == nil || raw.NextPage.Offset == "" {
			break
		}
		offset = raw.NextPage.Offset
	}
	return out, nil
}

// fetchGitLabIssues lists opened issues of "group/project". per_page ≤ 100,
// pages via Link rel="next". Self-hosted: PARATRACK_GITLAB_SITE.
// https://docs.gitlab.com/api/issues/ · https://docs.gitlab.com/api/rest/
func fetchGitLabIssues(token, project string) ([]extItem, error) {
	// Self-hosted: the site may lead the target ("https://git.acme.ru group/app").
	site := os.Getenv("PARATRACK_GITLAB_SITE")
	if f := strings.Fields(project); len(f) == 2 && strings.HasPrefix(f[0], "https://") {
		site, project = f[0], f[1]
	}
	if site == "" {
		site = "https://gitlab.com"
	}
	site = strings.TrimRight(site, "/")
	if project == "" {
		return nil, fmt.Errorf("gitlab project (group/project) is required")
	}
	next := site + "/api/v4/projects/" + url.PathEscape(project) + "/issues?state=opened&per_page=100"
	var out []extItem
	for next != "" && len(out) < maxImportTasks {
		var raw []struct {
			IID    int    `json:"iid"`
			Title  string `json:"title"`
			WebURL string `json:"web_url"`
			State  string `json:"state"`
		}
		h, err := getJSON("gitlab", newReq("GET", next, "", "Authorization", "Bearer "+token), &raw)
		if err != nil {
			return nil, err
		}
		for _, i := range raw {
			out = append(out, extItem{ID: fmt.Sprintf("gitlab-%d", i.IID), Title: fmt.Sprintf("#%d %s", i.IID, i.Title), URL: i.WebURL, Status: i.State})
		}
		next = linkNext(h)
	}
	return out, nil
}

// fetchClickUpTasks lists open tasks of a list. page starts at 0, up to
// 100 per page, until last_page. Raw personal token in Authorization.
// https://developer.clickup.com/reference/gettasks
func fetchClickUpTasks(token, listID string) ([]extItem, error) {
	if listID == "" {
		return nil, fmt.Errorf("clickup list id is required")
	}
	var out []extItem
	for page := 0; len(out) < maxImportTasks; page++ {
		u := fmt.Sprintf("https://api.clickup.com/api/v2/list/%s/task?include_closed=false&order_by=updated&reverse=true&page=%d",
			url.PathEscape(listID), page)
		var raw struct {
			Tasks []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				URL    string `json:"url"`
				Status struct {
					Status string `json:"status"`
					Type   string `json:"type"`
				} `json:"status"`
			} `json:"tasks"`
			LastPage *bool `json:"last_page"`
		}
		if _, err := getJSON("clickup", newReq("GET", u, "", "Authorization", token), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Tasks {
			st := t.Status.Status
			if t.Status.Type == "closed" {
				st = "closed"
			}
			out = append(out, extItem{ID: "clickup-" + t.ID, Title: t.Name, URL: t.URL, Status: st})
		}
		if (raw.LastPage != nil && *raw.LastPage) || len(raw.Tasks) < 100 {
			break
		}
	}
	return out, nil
}

// fetchTodoistTasks lists active tasks (API v1, optionally of a project).
// limit ≤ 200, cursor = next_cursor until null. v1 has no url field.
// https://developer.todoist.com/api/v1/
func fetchTodoistTasks(token, projectID string) ([]extItem, error) {
	var out []extItem
	cursor := ""
	for len(out) < maxImportTasks {
		u := "https://api.todoist.com/api/v1/tasks?limit=200"
		if projectID != "" {
			u += "&project_id=" + url.QueryEscape(projectID)
		}
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var raw struct {
			Results []struct {
				ID      string `json:"id"`
				Content string `json:"content"`
				URL     string `json:"url"`
			} `json:"results"`
			NextCursor *string `json:"next_cursor"`
		}
		if _, err := getJSON("todoist", newReq("GET", u, "", "Authorization", "Bearer "+token), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Results {
			link := t.URL
			if link == "" {
				link = "https://app.todoist.com/app/task/" + t.ID
			}
			out = append(out, extItem{ID: "todoist-" + t.ID, Title: t.Content, URL: link, Status: "open"})
		}
		if raw.NextCursor == nil || *raw.NextCursor == "" {
			break
		}
		cursor = *raw.NextCursor
	}
	return out, nil
}
