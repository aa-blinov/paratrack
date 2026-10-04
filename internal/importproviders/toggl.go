package importproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpretry"
	"github.com/aa-blinov/paratrack/internal/importport"
)

type togglReportQuery struct {
	StartDate      string `json:"start_date"`
	EndDate        string `json:"end_date"`
	PageSize       int    `json:"page_size"`
	FirstRowNumber *int   `json:"first_row_number,omitempty"`
}

// fetchTogglEntries pulls finished time entries from Toggl Track.
// secret = API token (Profile → API token), Basic auth token:api_token.
// Recent ranges use v9 /me/time_entries (no pagination; it only reaches
// about three months back). Older starts go through Reports API v3,
// paged by X-Next-Row-Number → first_row_number.
// https://engineering.toggl.com/docs/track/api/time_entries/ ·
// https://engineering.toggl.com/docs/track/reports/detailed_reports/
func fetchTogglEntries(client *http.Client, ctx context.Context, token string, from, to, now time.Time) ([]importedEntry, error) {
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
		if err == nil {
			return nil
		}
		var statusErr *httpretry.StatusError
		if !errors.As(err, &statusErr) || statusErr.Vendor != "toggl" {
			return err
		}
		switch statusErr.StatusCode {
		case http.StatusPaymentRequired:
			return fmt.Errorf("toggl: hourly API quota used up (402), try again in an hour")
		case http.StatusForbidden:
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
	if from.After(now.AddDate(0, -3, 0)) {
		u := fmt.Sprintf("https://api.track.toggl.com/api/v9/me/time_entries?start_date=%s&end_date=%s",
			url.QueryEscape(from.Format("2006-01-02")), url.QueryEscape(to.Format("2006-01-02")))
		var raw []struct {
			ID          int64   `json:"id"`
			Description string  `json:"description"`
			Start       string  `json:"start"`
			Stop        *string `json:"stop"`
		}
		if _, err := requestJSON(ctx, client, "toggl", mk("GET", u, ""), &raw); err != nil {
			return nil, togglErr(err)
		}
		if len(raw) > importport.MaxEntries {
			return nil, ErrEntryLimit
		}
		for _, e := range raw {
			if e.Stop == nil {
				continue
			}
			if en, ok := finish(e.ID, e.Description, e.Start, *e.Stop); ok {
				next, appendErr := appendImportedEntry(out, en)
				if appendErr != nil {
					return nil, appendErr
				}
				out = next
			}
		}
		return out, nil
	}
	var me struct {
		WorkspaceID int64 `json:"default_workspace_id"`
	}
	if _, err := requestJSON(ctx, client, "toggl", mk("GET", "https://api.track.toggl.com/api/v9/me", ""), &me); err != nil {
		return nil, togglErr(err)
	}
	row := 0
	complete := false
	for page := 0; page < 200; page++ {
		body := togglReportQuery{
			StartDate: from.Format("2006-01-02"),
			EndDate:   to.AddDate(0, 0, -1).Format("2006-01-02"), // inclusive here
			PageSize:  50,
		}
		if row > 0 {
			body.FirstRowNumber = &row
		}
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode Toggl report query: %w", err)
		}
		var rows []struct {
			Description string `json:"description"`
			TimeEntries []struct {
				ID    int64  `json:"id"`
				Start string `json:"start"`
				Stop  string `json:"stop"`
			} `json:"time_entries"`
		}
		h, err := requestJSON(ctx, client, "toggl", mk("POST",
			fmt.Sprintf("https://api.track.toggl.com/reports/api/v3/workspace/%d/search/time_entries", me.WorkspaceID), string(b)), &rows)
		if err != nil {
			return nil, togglErr(err)
		}
		for _, r := range rows {
			for _, te := range r.TimeEntries {
				if en, ok := finish(te.ID, r.Description, te.Start, te.Stop); ok {
					out, err = appendImportedEntry(out, en)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		next, _ := strconv.Atoi(h.Get("X-Next-Row-Number"))
		if next <= row || next == 0 {
			complete = true
			break
		}
		row = next
	}
	if !complete {
		return nil, fmt.Errorf("%w: toggl", ErrPaginationLimit)
	}
	return out, nil
}

func appendImportedEntry(entries []importedEntry, entry importedEntry) ([]importedEntry, error) {
	if len(entries) >= importport.MaxEntries {
		return nil, ErrEntryLimit
	}
	return append(entries, entry), nil
}
