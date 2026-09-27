package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the browser zone must load in a slim container

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------------------------------------------------------------------------
// Wave 8: PWA install + time-entry import (Toggl / Harvest / Clockify)
// ---------------------------------------------------------------------------

// handlePWA registers the service worker path (served from static) —
// this handler is the /sw.js alias if needed; the file is embedded.

// importedEntry is one external time entry ready to become a session.
type importedEntry struct {
	ExtID    string // "toggl:123": the entry's id in the source tracker
	Activity string
	Start    time.Time
	End      time.Time
	Note     string
}

// handleImport renders the migration page.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := importPage{pageData: pageData{Title: "Import", Active: "import", Lang: lang}}
	s.renderPageForRequest(w, r, "Import", "import", "import", &data)
}

type importPage struct {
	pageData
	Provider string
	From, To string
	Secret   string
	Extra    string
	TZ       string
	Entries  []importedEntry
	Error    string
}

func (p *importPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleImportPreview fetches entries and shows them. Rendered straight
// from the POST: a redirect put the provider token into the URL (history,
// proxy logs).
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f := func(k string) string { return strings.TrimSpace(r.PostForm.Get(k)) }
	data := importPage{pageData: pageData{Title: "Import", Active: "import", Lang: string(resolveLang(r))}}
	entries, err := fetchEntries(f("provider"), f("secret"), f("extra"), f("from"), f("to"), f("tz"))
	if err != nil {
		data.Error = err.Error()
	} else {
		data.Entries = entries
		data.Provider = f("provider")
		data.From, data.To, data.Secret, data.Extra, data.TZ = f("from"), f("to"), f("secret"), f("extra"), f("tz")
	}
	s.renderPageForRequest(w, r, "Import", "import", "import", &data)
}

// handleImportRun creates closed sessions from the fetched entries.
func (s *Server) handleImportRun(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	provider := strings.TrimSpace(r.PostForm.Get("provider"))
	secret := strings.TrimSpace(r.PostForm.Get("secret"))
	extra := strings.TrimSpace(r.PostForm.Get("extra"))
	from := strings.TrimSpace(r.PostForm.Get("from"))
	to := strings.TrimSpace(r.PostForm.Get("to"))
	entries, err := fetchEntries(provider, secret, extra, from, to, strings.TrimSpace(r.PostForm.Get("tz")))
	if err != nil {
		http.Redirect(w, r, "/import?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	n, skipped := 0, 0
	for _, e := range entries {
		if e.ExtID != "" && s.db.ImportedSessionExists(r.Context(), teamID(r), e.ExtID) {
			skipped++ // already imported earlier
			continue
		}
		act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), e.Activity)
		if err != nil {
			continue
		}
		if sess, err := s.db.CreateClosedSession(r.Context(), teamID(r), act.ID, e.Start, e.End, e.Note); err == nil {
			n++
			if e.ExtID != "" {
				_ = s.db.MarkImported(r.Context(), teamID(r), sess.ID, e.ExtID)
			}
		}
	}
	s.audit(r, "import.run", provider, fmt.Sprintf("%d", n))
	s.fireWebhook(r, "import.completed", map[string]any{"provider": provider, "imported": n})
	msg := fmt.Sprintf(i18n.T(resolveLang(r), "imp.done"), n, skipped)
	http.Redirect(w, r, "/stats?flash="+url.QueryEscape(encodeFlash(true, msg)), http.StatusSeeOther)
}

// fetchEntries dispatches to the provider importer.
// tz is the browser's IANA zone: "1 Sep" means the user's day, not UTC's.
func fetchEntries(provider, secret, extra, from, to, tz string) ([]importedEntry, error) {
	fromT, toT, err := parseImportRange(from, to, tz)
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

func parseImportRange(from, to, tz string) (time.Time, time.Time, error) {
	loc := time.Local
	if l, err := time.LoadLocation(tz); tz != "" && err == nil {
		loc = l
	}
	now := time.Now().In(loc)
	fromT := now.AddDate(0, 0, -30)
	toT := now
	if from != "" {
		t, err := time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return fromT, toT, fmt.Errorf("bad from date")
		}
		fromT = t
	}
	if to != "" {
		t, err := time.ParseInLocation("2006-01-02", to, loc)
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

// fetchTogglEntries pulls finished time entries from Toggl Track.
// secret = API token (Profile → API token), Basic auth token:api_token.
// Recent ranges use v9 /me/time_entries (no pagination; it only reaches
// about three months back). Older starts go through Reports API v3,
// paged by X-Next-Row-Number → first_row_number.
// https://engineering.toggl.com/docs/track/api/time_entries/ ·
// https://engineering.toggl.com/docs/track/reports/detailed_reports/
func fetchTogglEntries(token string, from, to time.Time) ([]importedEntry, error) {
	auth := func(req *http.Request) { req.SetBasicAuth(token, "api_token") }
	mk := func(method, u, body string) func() (*http.Request, error) {
		return func() (*http.Request, error) {
			req, err := newReq(method, u, body, "Content-Type", "application/json")()
			if err == nil {
				auth(req)
			}
			return req, err
		}
	}
	togglErr := func(err error) error {
		switch {
		case err == nil:
			return nil
		case strings.Contains(err.Error(), "toggl 402"):
			return fmt.Errorf("toggl: hourly API quota used up (402), try again in an hour")
		case strings.Contains(err.Error(), "toggl 403"):
			return fmt.Errorf("toggl: the API token was rejected (403)")
		}
		return err
	}
	finish := func(id int64, desc, start, stop string) (importedEntry, bool) {
		st, err1 := time.Parse(time.RFC3339, start)
		en, err2 := time.Parse(time.RFC3339, stop)
		if stop == "" || err1 != nil || err2 != nil {
			return importedEntry{}, false // running or unreadable
		}
		if desc == "" {
			desc = "imported"
		}
		return importedEntry{ExtID: fmt.Sprintf("toggl:%d", id), Activity: desc, Start: st, End: en, Note: "toggl"}, true
	}
	var out []importedEntry
	if from.After(time.Now().AddDate(0, -3, 0)) {
		u := fmt.Sprintf("https://api.track.toggl.com/api/v9/me/time_entries?start_date=%s&end_date=%s",
			url.QueryEscape(from.Format("2006-01-02")), url.QueryEscape(to.Format("2006-01-02")))
		var raw []struct {
			ID          int64   `json:"id"`
			Description string  `json:"description"`
			Start       string  `json:"start"`
			Stop        *string `json:"stop"`
		}
		if _, err := getJSON("toggl", mk("GET", u, ""), &raw); err != nil {
			return nil, togglErr(err)
		}
		for _, e := range raw {
			if e.Stop == nil {
				continue
			}
			if en, ok := finish(e.ID, e.Description, e.Start, *e.Stop); ok {
				out = append(out, en)
			}
		}
		return out, nil
	}
	var me struct {
		WorkspaceID int64 `json:"default_workspace_id"`
	}
	if _, err := getJSON("toggl", mk("GET", "https://api.track.toggl.com/api/v9/me", ""), &me); err != nil {
		return nil, togglErr(err)
	}
	row := 0
	for page := 0; page < 200; page++ {
		body := map[string]any{
			"start_date": from.Format("2006-01-02"),
			"end_date":   to.AddDate(0, 0, -1).Format("2006-01-02"), // inclusive here
			"page_size":  50,
		}
		if row > 0 {
			body["first_row_number"] = row
		}
		b, _ := json.Marshal(body)
		var rows []struct {
			Description string `json:"description"`
			TimeEntries []struct {
				ID    int64  `json:"id"`
				Start string `json:"start"`
				Stop  string `json:"stop"`
			} `json:"time_entries"`
		}
		h, err := getJSON("toggl", mk("POST",
			fmt.Sprintf("https://api.track.toggl.com/reports/api/v3/workspace/%d/search/time_entries", me.WorkspaceID), string(b)), &rows)
		if err != nil {
			return nil, togglErr(err)
		}
		for _, r := range rows {
			for _, te := range r.TimeEntries {
				if en, ok := finish(te.ID, r.Description, te.Start, te.Stop); ok {
					out = append(out, en)
				}
			}
		}
		next, _ := strconv.Atoi(h.Get("X-Next-Row-Number"))
		if next <= row || next == 0 {
			break
		}
		row = next
	}
	return out, nil
}

// fetchHarvestEntries pulls time entries via Harvest v2, every page.
// secret = access token; extra = account id (Harvest-Account-Id).
// Harvest's "to" is inclusive; our range end is exclusive.
func fetchHarvestEntries(token, accountID string, from, to time.Time) ([]importedEntry, error) {
	if accountID == "" {
		return nil, fmt.Errorf("harvest account id is required (extra field)")
	}
	type entry struct {
		ID          int64   `json:"id"`
		Notes       string  `json:"notes"`
		Hours       float64 `json:"hours"`
		SpentDate   string  `json:"spent_date"`
		StartedTime string  `json:"started_time"` // "8:00am" or "08:00", when the account tracks times
		EndedTime   string  `json:"ended_time"`
		IsRunning   bool    `json:"is_running"`
	}
	var all []entry
	next := fmt.Sprintf("https://api.harvestapp.com/v2/time_entries?from=%s&to=%s",
		from.Format("2006-01-02"), to.AddDate(0, 0, -1).Format("2006-01-02"))
	for page := 0; next != "" && page < 100; page++ {
		var raw struct {
			TimeEntries []entry `json:"time_entries"`
			Links       struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if _, err := getJSON("harvest", newReq("GET", next, "",
			"Authorization", "Bearer "+token, "Harvest-Account-Id", accountID, "User-Agent", "paratrack (https://paratrack.duckdns.org)"), &raw); err != nil {
			return nil, err
		}
		all = append(all, raw.TimeEntries...)
		next = ""
		if raw.Links.Next != nil {
			next = *raw.Links.Next
		}
	}
	out := make([]importedEntry, 0, len(all))
	cursor := map[string]time.Time{} // entries without clock times stack from 09:00
	for _, e := range all {
		if e.IsRunning {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", e.SpentDate, from.Location())
		if err != nil {
			continue
		}
		dur := time.Duration(e.Hours * float64(time.Hour))
		start, ok := harvestClock(day, e.StartedTime)
		if !ok {
			c, seen := cursor[e.SpentDate]
			if !seen {
				c = day.Add(9 * time.Hour)
			}
			start = c
			cursor[e.SpentDate] = c.Add(dur)
		}
		name := e.Notes
		if name == "" {
			name = "imported"
		}
		out = append(out, importedEntry{ExtID: fmt.Sprintf("harvest:%d", e.ID), Activity: name, Start: start, End: start.Add(dur), Note: "harvest"})
	}
	return out, nil
}

// harvestClock reads Harvest's started_time in either account format.
func harvestClock(day time.Time, s string) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, layout := range []string{"3:04pm", "15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return day.Add(time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute), true
		}
	}
	return time.Time{}, false
}

// fetchClockifyEntries pulls the key owner's time entries via Clockify
// API v1. The list lives under the user (/workspaces/{ws}/user/{id}/…);
// the workspace defaults to the user's active one.
func fetchClockifyEntries(apiKey, workspaceID string, from, to time.Time) ([]importedEntry, error) {
	get := func(u string, into any) (http.Header, error) {
		return getJSON("clockify", newReq("GET", u, "", "X-Api-Key", apiKey), into)
	}
	var me struct {
		ID              string `json:"id"`
		ActiveWorkspace string `json:"activeWorkspace"`
	}
	if _, err := get("https://api.clockify.me/api/v1/user", &me); err != nil {
		return nil, err
	}
	if workspaceID == "" {
		workspaceID = me.ActiveWorkspace
	}
	type entry struct {
		ID           string `json:"id"`
		Description  string `json:"description"`
		TimeInterval struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"timeInterval"`
	}
	const size = 200
	var all []entry
	for page := 1; page <= 100; page++ {
		var batch []entry
		u := fmt.Sprintf("https://api.clockify.me/api/v1/workspaces/%s/user/%s/time-entries?start=%s&end=%s&page=%d&page-size=%d",
			url.PathEscape(workspaceID), url.PathEscape(me.ID),
			url.QueryEscape(from.UTC().Format(time.RFC3339)), url.QueryEscape(to.UTC().Format(time.RFC3339)), page, size)
		h, err := get(u, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		// Clockify says when it's done in a Last-Page header.
		if h.Get("Last-Page") == "true" || len(batch) < size {
			break
		}
	}
	out := make([]importedEntry, 0, len(all))
	for _, e := range all {
		if e.TimeInterval.End == "" {
			continue // running: nothing to import yet
		}
		start, err1 := time.Parse(time.RFC3339, e.TimeInterval.Start)
		end, err2 := time.Parse(time.RFC3339, e.TimeInterval.End)
		if err1 != nil || err2 != nil {
			continue
		}
		name := e.Description
		if name == "" {
			name = "imported"
		}
		out = append(out, importedEntry{ExtID: "clockify:" + e.ID, Activity: name, Start: start, End: end, Note: "clockify"})
	}
	return out, nil
}

var _ = timeparse.Period{}
var _ = strconv.Itoa
