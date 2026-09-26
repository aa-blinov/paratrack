package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------------------------------------------------------------------------
// Wave 8: PWA install + time-entry import (Toggl / Harvest / Clockify)
// ---------------------------------------------------------------------------

// handlePWA registers the service worker path (served from static) —
// this handler is the /sw.js alias if needed; the file is embedded.

// importedEntry is one external time entry ready to become a session.
type importedEntry struct {
	Activity string
	Start    time.Time
	End      time.Time
	Note     string
}

// handleImport renders the migration page.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := importPage{pageData: pageData{Title: "Import", Active: "import", Lang: lang}}
	if v := r.URL.Query().Get("preview"); v != "" {
		// preview mode: run the fetch and render the table
		provider := r.URL.Query().Get("provider")
		secret := r.URL.Query().Get("secret")
		extra := r.URL.Query().Get("extra")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		entries, err := fetchEntries(provider, secret, extra, from, to)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Entries = entries
			data.Provider = provider
			data.From, data.To, data.Secret, data.Extra = from, to, secret, extra
		}
	}
	s.renderPageForRequest(w, r, "Import", "import", "import", &data)
}

type importPage struct {
	pageData
	Provider string
	From, To string
	Secret   string
	Extra    string
	Entries  []importedEntry
	Error    string
}

func (p *importPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleImportPreview fetches entries and shows them (form POST).
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	q := url.Values{}
	q.Set("preview", "1")
	q.Set("provider", strings.TrimSpace(r.PostForm.Get("provider")))
	q.Set("secret", strings.TrimSpace(r.PostForm.Get("secret")))
	q.Set("extra", strings.TrimSpace(r.PostForm.Get("extra")))
	q.Set("from", strings.TrimSpace(r.PostForm.Get("from")))
	q.Set("to", strings.TrimSpace(r.PostForm.Get("to")))
	http.Redirect(w, r, "/import?"+q.Encode(), http.StatusSeeOther)
}

// handleImportRun creates closed sessions from the fetched entries.
func (s *Server) handleImportRun(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	provider := strings.TrimSpace(r.PostForm.Get("provider"))
	secret := strings.TrimSpace(r.PostForm.Get("secret"))
	extra := strings.TrimSpace(r.PostForm.Get("extra"))
	from := strings.TrimSpace(r.PostForm.Get("from"))
	to := strings.TrimSpace(r.PostForm.Get("to"))
	entries, err := fetchEntries(provider, secret, extra, from, to)
	if err != nil {
		http.Redirect(w, r, "/import?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	n := 0
	for _, e := range entries {
		act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), e.Activity)
		if err != nil {
			continue
		}
		if _, err := s.db.CreateClosedSession(r.Context(), teamID(r), act.ID, e.Start, e.End, e.Note); err == nil {
			n++
		}
	}
	s.audit(r, "import.run", provider, fmt.Sprintf("%d", n))
	s.fireWebhook(r, "import.completed", map[string]any{"provider": provider, "imported": n})
	http.Redirect(w, r, "/stats?flash="+encodeFlash(true, fmt.Sprintf("imported %d entries", n)), http.StatusSeeOther)
}

// fetchEntries dispatches to the provider importer.
func fetchEntries(provider, secret, extra, from, to string) ([]importedEntry, error) {
	fromT, toT, err := parseImportRange(from, to)
	if err != nil {
		return nil, err
	}
	switch provider {
	case "toggl":
		return fetchTogglEntries(secret, fromT, toT)
	case "harvest":
		return fetchHarvestEntries(secret, extra, fromT, toT)
	case "clockify":
		return fetchClockifyEntries(secret, extra, fromT, toT)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

func parseImportRange(from, to string) (time.Time, time.Time, error) {
	now := time.Now()
	fromT := now.AddDate(0, 0, -30)
	toT := now
	if from != "" {
		t, err := time.Parse("2006-01-02", from)
		if err != nil {
			return fromT, toT, fmt.Errorf("bad from date")
		}
		fromT = t
	}
	if to != "" {
		t, err := time.Parse("2006-01-02", to)
		if err != nil {
			return fromT, toT, fmt.Errorf("bad to date")
		}
		toT = t.AddDate(0, 0, 1)
	}
	if !toT.After(fromT) {
		return fromT, toT, fmt.Errorf("to must be after from")
	}
	return fromT, toT, nil
}

// fetchTogglEntries pulls time entries via Toggl API v9.
// secret = API token (Toggl "My Profile" → API token).
func fetchTogglEntries(token string, from, to time.Time) ([]importedEntry, error) {
	u := fmt.Sprintf("https://api.track.toggl.com/api/v9/me/time_entries?start_date=%s&end_date=%s",
		url.QueryEscape(from.Format("2006-01-02")), url.QueryEscape(to.Format("2006-01-02")))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(token, "api_token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("toggl %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var raw []struct {
		ID          int64   `json:"id"`
		Description string  `json:"description"`
		Start       string  `json:"start"`
		Stop        *string `json:"stop"`
		ProjectID   *int64  `json:"project_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]importedEntry, 0, len(raw))
	for _, e := range raw {
		if e.Stop == nil {
			continue // skip running entries
		}
		start, err := time.Parse(time.RFC3339, e.Start)
		if err != nil {
			continue
		}
		end, err := time.Parse(time.RFC3339, *e.Stop)
		if err != nil {
			continue
		}
		name := e.Description
		if name == "" {
			name = "imported"
		}
		out = append(out, importedEntry{Activity: name, Start: start, End: end, Note: "toggl"})
	}
	return out, nil
}

// fetchHarvestEntries pulls time entries via Harvest v2.
// secret = access token; extra = account id (X-Harvest-Account-Id).
func fetchHarvestEntries(token, accountID string, from, to time.Time) ([]importedEntry, error) {
	if accountID == "" {
		return nil, fmt.Errorf("harvest account id is required (extra field)")
	}
	u := fmt.Sprintf("https://api.harvestapp.com/v2/time_entries?from=%s&to=%s&per_page=100",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Harvest-Account-Id", accountID)
	req.Header.Set("User-Agent", "paratrack")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("harvest %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var raw struct {
		TimeEntries []struct {
			ID       int64   `json:"id"`
			Notes    string  `json:"notes"`
			Hours    float64 `json:"hours"`
			SpentDate string `json:"spent_date"`
		} `json:"time_entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]importedEntry, 0, len(raw.TimeEntries))
	for _, e := range raw.TimeEntries {
		day, err := time.Parse("2006-01-02", e.SpentDate)
		if err != nil {
			continue
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, day.Location())
		end := start.Add(time.Duration(e.Hours * float64(time.Hour)))
		name := e.Notes
		if name == "" {
			name = "imported"
		}
		out = append(out, importedEntry{Activity: name, Start: start, End: end, Note: "harvest"})
	}
	return out, nil
}

// fetchClockifyEntries pulls time entries via Clockify API.
// secret = API key; extra = workspace id.
func fetchClockifyEntries(apiKey, workspaceID string, from, to time.Time) ([]importedEntry, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("clockify workspace id is required (extra field)")
	}
	u := fmt.Sprintf("https://api.clockify.me/api/v1/workspaces/%s/time-entries?start=%s&end=%s&hydrated=true&page-size=100",
		url.PathEscape(workspaceID),
		url.QueryEscape(from.Format(time.RFC3339)),
		url.QueryEscape(to.Format(time.RFC3339)))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("clockify %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var raw []struct {
		ID       string  `json:"id"`
		Description string `json:"description"`
		TimeInterval struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"timeInterval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]importedEntry, 0, len(raw))
	for _, e := range raw {
		start, err := time.Parse(time.RFC3339, e.TimeInterval.Start)
		if err != nil {
			continue
		}
		var end time.Time
		if e.TimeInterval.End != "" {
			end, _ = time.Parse(time.RFC3339, e.TimeInterval.End)
		} else {
			end = start.Add(time.Hour)
		}
		name := e.Description
		if name == "" {
			name = "imported"
		}
		out = append(out, importedEntry{Activity: name, Start: start, End: end, Note: "clockify"})
	}
	return out, nil
}

var _ = timeparse.Period{}
var _ = strconv.Itoa
