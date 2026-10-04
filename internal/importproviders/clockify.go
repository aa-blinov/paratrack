package importproviders

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/aa-blinov/paratrack/internal/importport"
)

// fetchClockifyEntries pulls the key owner's time entries via Clockify
// API v1. The list lives under the user (/workspaces/{ws}/user/{id}/…);
// the workspace defaults to the user's active one.
func fetchClockifyEntries(client *http.Client, ctx context.Context, apiKey, workspaceID string, from, to time.Time) ([]importedEntry, error) {
	get := func(u string, into any) (http.Header, error) {
		return requestJSON(ctx, client, "clockify", newReq("GET", u, "", "X-Api-Key", apiKey), into)
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
	complete := false
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
		if len(all) > importport.MaxEntries {
			return nil, ErrEntryLimit
		}
		// Clockify says when it's done in a Last-Page header.
		if h.Get("Last-Page") == "true" || len(batch) < size {
			complete = true
			break
		}
	}
	if !complete {
		return nil, fmt.Errorf("%w: clockify", ErrPaginationLimit)
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
