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

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// ---------------------------------------------------------------------------
// Wave 2: integrations + API tokens
// ---------------------------------------------------------------------------

// handleSettingsTokens renders /settings/tokens.
func (s *Server) handleSettingsTokens(w http.ResponseWriter, r *http.Request) {
	s.renderTokens(w, r, "")
}

// renderTokens shows the token list; justCreated is the raw value of a
// token minted by this very request, shown once and never put in a URL.
func (s *Server) renderTokens(w http.ResponseWriter, r *http.Request, justCreated string) {
	u, _ := UserFrom(r.Context())
	list, _ := s.db.ListAPITokens(r.Context(), u.ID)
	lang := string(resolveLang(r))
	data := tokensPage{
		pageData: pageData{Title: "API tokens", Active: "settings-tokens", Lang: lang},
	}
	for _, t := range list {
		row := dbTokenRow{
			ID: t.ID, Name: t.Name, Prefix: t.Prefix, ReadOnly: t.ReadOnly,
			Created: fmtDate(resolveLang(r), t.CreatedAt.Local()),
		}
		if t.ExpiresAt != nil {
			row.Expires = fmtDate(resolveLang(r), t.ExpiresAt.Local())
			row.Expired = !t.ExpiresAt.After(time.Now())
		}
		if t.TeamID > 0 {
			if tm, err := s.teams.FindByID(r.Context(), t.TeamID); err == nil {
				row.Team = tm.Name
			}
		}
		data.Tokens = append(data.Tokens, row)
	}
	if justCreated != "" {
		data.JustCreated = justCreated
		w.Header().Set("Cache-Control", "no-store")
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "API tokens", "settings-tokens", "tokens", &data)
}

type dbTokenRow struct {
	ID       int64
	Name     string
	Prefix   string
	Created  string
	Expires  string // "" = never
	Expired  bool
	Team     string
	ReadOnly bool
}

// tokensPage is the /settings/tokens envelope.
type tokensPage struct {
	pageData
	Tokens      []dbTokenRow
	JustCreated string
	Flash       string
	FlashOK     bool
}

func (p *tokensPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleAPITokenCreate mints a token and shows its raw value once, in the
// response itself (a redirect carried it in the URL: history, logs).
func (s *Server) handleAPITokenCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u, _ := UserFrom(r.Context())
	name := strings.TrimSpace(r.PostForm.Get("name"))
	opts := db.TokenOptions{TeamID: teamID(r), ReadOnly: r.PostForm.Get("read_only") == "1"}
	if days, _ := strconv.Atoi(r.PostForm.Get("expires_days")); days > 0 {
		t := time.Now().AddDate(0, 0, days)
		opts.ExpiresAt = &t
	}
	raw, _, err := s.db.CreateAPIToken(r.Context(), u.ID, name, opts)
	if err != nil {
		http.Redirect(w, r, "/settings/tokens?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.renderTokens(w, r, raw)
}

func (s *Server) handleAPITokenDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/tokens/"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	u, _ := UserFrom(r.Context())
	_ = s.db.DeleteAPIToken(r.Context(), u.ID, id)
	http.Redirect(w, r, "/settings/tokens?flash=removed", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Integrations pages
// ---------------------------------------------------------------------------

// handleIntegrations lists connected providers and the connect form.
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	list, _ := s.db.ListIntegrations(r.Context(), teamID(r))
	lang := string(resolveLang(r))
	data := integrationsPage{
		pageData: pageData{Title: "Integrations", Active: "integrations", Lang: lang},
	}
	for _, it := range list {
		n, _ := s.db.ListExternalTasks(r.Context(), it.ID)
		data.Items = append(data.Items, integrationRow{
			ID: it.ID, Provider: it.Provider, Name: it.Name,
			TaskCount: len(n),
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Integrations", "integrations", "integrations", &data)
}

type integrationRow struct {
	ID        int64
	Provider  string
	Name      string
	TaskCount int
}

// integrationsPage is the /integrations envelope.
type integrationsPage struct {
	pageData
	Items   []integrationRow
	Flash   string
	FlashOK bool
}

func (p *integrationsPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleIntegrationDetail lists imported tasks with a one-click Start.
func (s *Server) handleIntegrationDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/integrations/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	it, err := s.db.GetIntegration(r.Context(), teamID(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tasks, _ := s.db.ListExternalTasks(r.Context(), it.ID)
	lang := string(resolveLang(r))
	data := integrationDetailPage{
		pageData:    pageData{Title: it.Name, Active: "integrations", Lang: lang},
		Integration: integrationRow{ID: it.ID, Provider: it.Provider, Name: it.Name, TaskCount: len(tasks)},
	}
	for _, t := range tasks {
		data.Tasks = append(data.Tasks, dbTaskRow2{
			ID: t.ID, Title: t.Title, URL: t.URL, Status: t.Status, ExternalID: t.ExternalID,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, it.Name, "integrations", "integration-detail", &data)
}

type dbTaskRow2 struct {
	ID         int64
	Title      string
	URL        string
	Status     string
	ExternalID string
}

// integrationDetailPage is the /integrations/{id} envelope.
type integrationDetailPage struct {
	pageData
	Integration integrationRow
	Tasks       []dbTaskRow2
	Flash       string
	FlashOK     bool
}

func (p *integrationDetailPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleIntegrationConnect stores a credential and imports items.
//
// Form: provider=github|trello, name, secret, extra (repo or board id).
func (s *Server) handleIntegrationConnect(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	provider := strings.TrimSpace(r.PostForm.Get("provider"))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	secret := strings.TrimSpace(r.PostForm.Get("secret"))
	extra := strings.TrimSpace(r.PostForm.Get("extra"))
	cfg := "{}"
	if extra != "" {
		b, _ := json.Marshal(map[string]string{"target": extra})
		cfg = string(b)
	}
	it, err := s.db.CreateIntegration(r.Context(), teamID(r), provider, name, secret, cfg)
	if err != nil {
		http.Redirect(w, r, "/integrations?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	n, ierr := s.importTasks(r, it)
	http.Redirect(w, r, fmt.Sprintf("/integrations/%d?flash=%s", it.ID, importFlash(r, n, ierr)), http.StatusSeeOther)
}

func (s *Server) handleIntegrationDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/integrations/"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	_ = s.db.DeleteIntegration(r.Context(), teamID(r), id)
	http.Redirect(w, r, "/integrations?flash=removed", http.StatusSeeOther)
}

func (s *Server) handleIntegrationSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/integrations/"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	it, err := s.db.GetIntegration(r.Context(), teamID(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	n, ierr := s.importTasks(r, it)
	http.Redirect(w, r, fmt.Sprintf("/integrations/%d?flash=%s", it.ID, importFlash(r, n, ierr)), http.StatusSeeOther)
}

// handleIntegrationStart starts a timer on an imported task's activity.
func (s *Server) handleIntegrationStart(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tid, err := strconv.ParseInt(r.PostForm.Get("task_id"), 10, 64)
	if err != nil {
		http.Error(w, "task_id required", 400)
		return
	}
	tasks, err := s.db.ListExternalTasks(r.Context(), 0)
	_ = tasks
	_ = tid
	// Resolve via integration list instead (team-scoped).
	var title string
	list, _ := s.db.ListIntegrations(r.Context(), teamID(r))
	for _, it := range list {
		ts, _ := s.db.ListExternalTasks(r.Context(), it.ID)
		for _, t := range ts {
			if t.ID == tid {
				title = t.Title
			}
		}
	}
	if title == "" {
		http.NotFound(w, r)
		return
	}
	// Use the task title as the activity name so the timer is labelled.
	act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), title)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Reuse handleStart's body shape.
	r.Form.Set("activity", act.Name)
	s.handleStart(w, r)
}

// ---------------------------------------------------------------------------
// Provider importers
// ---------------------------------------------------------------------------

// importTasks pulls open items from the provider into external_tasks.
func (s *Server) importTasks(r *http.Request, it db.Integration) (int, error) {
	var items []extItem
	var err error
	target := ""
	var cfg struct {
		Target string `json:"target"`
	}
	_ = json.Unmarshal([]byte(it.Config), &cfg)
	target = cfg.Target
	switch it.Provider {
	case "github":
		items, err = fetchGitHubIssues(it.Secret, target)
	case "trello":
		items, err = fetchTrelloCards(it.Secret, target)
	case "jira":
		items, err = fetchJiraIssues(it.Secret, target)
	case "notion":
		items, err = fetchNotionTasks(it.Secret, target)
	case "asana":
		items, err = fetchAsanaTasks(it.Secret, target)
	case "gitlab":
		items, err = fetchGitLabIssues(it.Secret, target)
	case "clickup":
		items, err = fetchClickUpTasks(it.Secret, target)
	case "todoist":
		items, err = fetchTodoistTasks(it.Secret, target)
	default:
		return 0, fmt.Errorf("unknown provider %q", it.Provider)
	}
	if err != nil {
		return 0, err
	}
	n := 0
	keep := make([]string, 0, len(items))
	for _, item := range items {
		if _, err := s.db.UpsertExternalTask(r.Context(), it.ID, item.ID, item.Title, item.URL, item.Status); err == nil {
			n++
			keep = append(keep, item.ID)
		}
	}
	// The fetch returns open items only: whatever is gone was closed there.
	if err := s.db.CloseMissingExternalTasks(r.Context(), it.ID, keep); err != nil {
		return n, err
	}
	return n, nil
}

// importFlash is the result banner after connect / sync.
func importFlash(r *http.Request, n int, err error) string {
	lang := resolveLang(r)
	if err != nil {
		return url.QueryEscape(encodeFlash(false, fmt.Sprintf(i18n.T(lang, "int.importFailed"), err.Error())))
	}
	return url.QueryEscape(encodeFlash(true, fmt.Sprintf(i18n.T(lang, "int.imported"), n)))
}

type extItem struct {
	ID     string
	Title  string
	URL    string
	Status string
}

// fetchGitHubIssues lists open issues for "owner/repo" using a PAT.
func fetchGitHubIssues(token, repo string) ([]extItem, error) {
	if repo == "" {
		// fall back to the token owner's recent issues
		repo = ""
	}
	u := "https://api.github.com/issues?state=open&per_page=50"
	if repo != "" {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			u = fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=50",
				url.PathEscape(parts[0]), url.PathEscape(parts[1]))
		}
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "paratrack")
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
		Repo    struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest *struct{} `json:"pull_request"` // the issues API returns PRs too
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw))
	for _, i := range raw {
		if i.PullRequest != nil {
			continue
		}
		name := i.Title
		full := i.Repo.FullName
		if full != "" {
			name = full + "#" + strconv.Itoa(i.Number) + " " + i.Title
		} else {
			full = repo
		}
		out = append(out, extItem{
			// Numbers repeat across repos: the id carries the repo.
			ID:     fmt.Sprintf("gh-%s#%d", full, i.Number),
			Title:  name,
			URL:    i.HTMLURL,
			Status: i.State,
		})
	}
	return out, nil
}

// fetchTrelloCards lists open cards on a board using key+token in the
// secret field as "key:token".
func fetchTrelloCards(secret, board string) ([]extItem, error) {
	key, token, ok := strings.Cut(secret, ":")
	if !ok || key == "" || token == "" {
		return nil, fmt.Errorf("trello: the secret is key:token")
	}
	u := fmt.Sprintf("https://api.trello.com/1/boards/%s/cards?filter=open&key=%s&token=%s",
		url.PathEscape(board), url.QueryEscape(key), url.QueryEscape(token))
	resp, err := extClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trello %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		ShortURL string `json:"shortUrl"`
		Closed   bool   `json:"closed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw))
	for _, i := range raw {
		st := "open"
		if i.Closed {
			st = "closed"
		}
		out = append(out, extItem{ID: "tr-" + i.ID, Title: i.Name, URL: i.ShortURL, Status: st})
	}
	return out, nil
}

var jiraKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// fetchJiraIssues lists unresolved issues via Jira REST v3.
//
// secret = "email:apitoken" (Cloud API token) or a bare Bearer PAT.
// target = project key ("PROJ") or a JQL snippet ("project = PROJ AND ...").
func fetchJiraIssues(secret, target string) ([]extItem, error) {
	// The site may lead the target ("https://acme.atlassian.net PROJ"), so
	// each workspace can point at its own Jira; the env var is the default.
	site := os.Getenv("PARATRACK_JIRA_SITE")
	if f := strings.Fields(target); len(f) > 0 && strings.HasPrefix(f[0], "https://") {
		site, target = f[0], strings.TrimSpace(strings.TrimPrefix(target, f[0]))
	}
	if site == "" {
		return nil, fmt.Errorf("jira: put the site first in the target, e.g. https://acme.atlassian.net PROJ")
	}
	site = strings.TrimRight(site, "/")
	jql := target
	switch {
	case jql == "":
		jql = "resolution = Unresolved ORDER BY updated DESC"
	case jiraKey.MatchString(jql):
		jql = "project = " + jql + " AND resolution = Unresolved ORDER BY updated DESC"
	}
	// /rest/api/3/search was removed (410); /search/jql is its successor.
	u := site + "/rest/api/3/search/jql?jql=" + url.QueryEscape(jql) + "&maxResults=50&fields=summary,status,issuetype"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.Contains(secret, ":") {
		req.SetBasicAuth(strings.SplitN(secret, ":", 2)[0], strings.SplitN(secret, ":", 2)[1])
	} else {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw.Issues))
	for _, i := range raw.Issues {
		out = append(out, extItem{
			ID:     "jira-" + i.Key,
			Title:  i.Key + " " + i.Fields.Summary,
			URL:    site + "/browse/" + i.Key,
			Status: strings.ToLower(i.Fields.Status.Name),
		})
	}
	return out, nil
}

// fetchNotionTasks queries a database and returns its pages as tasks.
//
// secret = internal integration secret ("ntn_…" / "secret_…").
// target = database ID (32-char hex with or without dashes).
func fetchNotionTasks(secret, dbID string) ([]extItem, error) {
	if dbID == "" {
		return nil, fmt.Errorf("notion database id is required")
	}
	dbID = strings.ReplaceAll(dbID, "-", "")
	if len(dbID) != 32 {
		return nil, fmt.Errorf("notion database id must be 32 hex chars")
	}
	// pretty UUID for the API
	formatted := dbID[0:8] + "-" + dbID[8:12] + "-" + dbID[12:16] + "-" + dbID[16:20] + "-" + dbID[20:32]
	// No filter: a database needn't have a "Status" property (filtering on
	// one it lacks is a 400).
	body, _ := json.Marshal(map[string]any{"page_size": 100})
	req, err := http.NewRequest("POST",
		"https://api.notion.com/v1/databases/"+formatted+"/query", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("notion %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw struct {
		Results []struct {
			ID         string `json:"id"`
			URL        string `json:"url"`
			Properties map[string]struct {
				Title []struct {
					PlainText string `json:"plain_text"`
				} `json:"title"`
			} `json:"properties"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw.Results))
	for _, r := range raw.Results {
		title := "Untitled"
		for _, v := range r.Properties {
			if len(v.Title) > 0 && v.Title[0].PlainText != "" {
				title = v.Title[0].PlainText
				break
			}
		}
		out = append(out, extItem{
			ID:     "notion-" + r.ID,
			Title:  title,
			URL:    r.URL,
			Status: "open",
		})
	}
	return out, nil
}

// fetchAsanaTasks lists incomplete tasks in a project.
//
// secret = Personal Access Token (asana PAT).
// target = project GID (or workspace GID → first 50 projects' tasks is
// too broad, so we require the project GID).
func fetchAsanaTasks(token, projectGID string) ([]extItem, error) {
	if projectGID == "" {
		return nil, fmt.Errorf("asana project gid is required")
	}
	u := fmt.Sprintf("https://app.asana.com/api/1.0/projects/%s/tasks?opt_fields=name,completed,permalink_url&completed_since=now&limit=100",
		url.PathEscape(projectGID))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asana %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw struct {
		Data []struct {
			GID       string `json:"gid"`
			Name      string `json:"name"`
			Completed bool   `json:"completed"`
			Permalink string `json:"permalink_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw.Data))
	for _, t := range raw.Data {
		if t.Completed {
			continue
		}
		out = append(out, extItem{
			ID: "asana-" + t.GID, Title: t.Name, URL: t.Permalink, Status: "open",
		})
	}
	return out, nil
}

// fetchGitLabIssues lists open issues for "group/project".
//
// secret = PAT (scope api / read_api).
// target = "group/project" (or a bare numeric project id).
// site = PARATRACK_GITLAB_SITE (default https://gitlab.com).
func fetchGitLabIssues(token, project string) ([]extItem, error) {
	site := os.Getenv("PARATRACK_GITLAB_SITE")
	if site == "" {
		site = "https://gitlab.com"
	}
	site = strings.TrimRight(site, "/")
	if project == "" {
		return nil, fmt.Errorf("gitlab project (group/project) is required")
	}
	u := site + "/api/v4/projects/" + url.PathEscape(project) +
		"/issues?state=opened&per_page=50"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw []struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		WebURL string `json:"web_url"`
		State  string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw))
	for _, i := range raw {
		out = append(out, extItem{
			ID:    fmt.Sprintf("gitlab-%d", i.IID),
			Title: fmt.Sprintf("#%d %s", i.IID, i.Title),
			URL:   i.WebURL, Status: i.State,
		})
	}
	return out, nil
}

// fetchClickUpTasks lists open tasks in a list.
//
// secret = API token (pk_… / pk_pat_…).
// target = list id.
func fetchClickUpTasks(token, listID string) ([]extItem, error) {
	if listID == "" {
		return nil, fmt.Errorf("clickup list id is required")
	}
	u := fmt.Sprintf("https://api.clickup.com/api/v2/list/%s/task?include_closed=false&order_by=updated&reverse=true",
		url.PathEscape(listID))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clickup %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw.Tasks))
	for _, t := range raw.Tasks {
		st := t.Status.Status
		if t.Status.Type == "closed" {
			st = "closed"
		}
		out = append(out, extItem{ID: "clickup-" + t.ID, Title: t.Name, URL: t.URL, Status: st})
	}
	return out, nil
}

// fetchTodoistTasks lists active tasks (Sync or REST v2).
//
// secret = API token (todoist_pat).
// target = optional project id; empty → all active tasks (max 50).
func fetchTodoistTasks(token, projectID string) ([]extItem, error) {
	u := "https://api.todoist.com/api/v1/tasks?limit=50"
	if projectID != "" {
		u += "&project_id=" + url.QueryEscape(projectID)
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := extClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("todoist %d: %s", resp.StatusCode, string(b)[:min(200, len(b))])
	}
	var raw struct {
		Results []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			URL     string `json:"url"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]extItem, 0, len(raw.Results))
	for _, t := range raw.Results {
		link := t.URL
		if link == "" { // API v1 dropped the url field
			link = "https://app.todoist.com/app/task/" + t.ID
		}
		out = append(out, extItem{ID: "todoist-" + t.ID, Title: t.Content, URL: link, Status: "open"})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// JSON for the browser extension (Bearer auth)
// ---------------------------------------------------------------------------

// handleAPIMe returns the token owner — lets the extension confirm auth.
func (s *Server) handleAPIMe(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	team, _ := TeamFrom(r.Context())
	writeJSON(w, map[string]any{
		"user": map[string]any{"id": u.ID, "name": u.Name, "email": u.Email},
		"team": map[string]any{"id": team.ID, "name": team.Name},
	})
}

// handleAPIExternalTasks lists imported tasks across integrations.
func (s *Server) handleAPIExternalTasks(w http.ResponseWriter, r *http.Request) {
	list, _ := s.db.ListIntegrations(r.Context(), teamID(r))
	type row struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Status   string `json:"status"`
		Provider string `json:"provider"`
	}
	out := []row{}
	for _, it := range list {
		ts, _ := s.db.ListExternalTasks(r.Context(), it.ID)
		for _, t := range ts {
			out = append(out, row{ID: t.ID, Title: t.Title, URL: t.URL, Status: t.Status, Provider: it.Provider})
		}
	}
	writeJSON(w, map[string]any{"tasks": out})
}
