package importproviders

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpurl"
	"github.com/aa-blinov/paratrack/internal/importport"
)

// fetchHarvestEntries pulls time entries via Harvest v2, every page.
// secret = access token; extra = account id (Harvest-Account-Id).
// Harvest's "to" is inclusive; our range end is exclusive.
func fetchHarvestEntries(client *http.Client, ctx context.Context, token, accountID string, from, to time.Time) ([]importedEntry, error) {
	if accountID == "" {
		return nil, fmt.Errorf("%w: harvest account id is required", ErrInvalidInput)
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
		pageURL := next
		var raw struct {
			TimeEntries []entry `json:"time_entries"`
			Links       struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if _, err := requestJSON(ctx, client, "harvest", newReq("GET", pageURL, "",
			"Authorization", "Bearer "+token, "Harvest-Account-Id", accountID, "User-Agent", "paratrack (https://paratrack.duckdns.org)"), &raw); err != nil {
			return nil, err
		}
		all = append(all, raw.TimeEntries...)
		if len(all) > importport.MaxEntries {
			return nil, ErrEntryLimit
		}
		next = ""
		if raw.Links.Next != nil {
			resolved, err := httpurl.ResolveSameOrigin(pageURL, *raw.Links.Next)
			if err != nil {
				return nil, fmt.Errorf("harvest pagination link: %w", err)
			}
			next = resolved
		}
	}
	if next != "" {
		return nil, fmt.Errorf("%w: harvest", ErrPaginationLimit)
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
		dur, err := durationFromHours(e.Hours)
		if err != nil {
			return nil, fmt.Errorf("harvest entry %d: %w", e.ID, err)
		}
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

func durationFromHours(hours float64) (time.Duration, error) {
	nanoseconds := hours * float64(time.Hour)
	const maxInt64 = int64(^uint64(0) >> 1)
	if math.IsNaN(nanoseconds) || math.IsInf(nanoseconds, 0) || nanoseconds <= 0 || nanoseconds >= float64(maxInt64) {
		return 0, fmt.Errorf("duration is outside the supported range")
	}
	return time.Duration(nanoseconds), nil
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
