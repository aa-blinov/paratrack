package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
			Created: fmtDate(resolveLang(r), t.CreatedAt.In(userLoc(r))),
		}
		if t.ExpiresAt != nil {
			row.Expires = fmtDate(resolveLang(r), t.ExpiresAt.In(userLoc(r)))
			row.Expired = !t.ExpiresAt.After(userNow(r))
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
		t := userNow(r).AddDate(0, 0, days)
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	_ = s.db.DeleteIntegration(r.Context(), teamID(r), id)
	http.Redirect(w, r, "/integrations?flash=removed", http.StatusSeeOther)
}

func (s *Server) handleIntegrationSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
