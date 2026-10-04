// Package importproviders adapts external time tracking APIs to imported session records.
package importproviders

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/httpjson"
	"github.com/aa-blinov/paratrack/internal/httpretry"
	"github.com/aa-blinov/paratrack/internal/importport"
)

type importedEntry struct {
	ExtID    string
	Activity string
	Start    time.Time
	End      time.Time
	Note     string
}

var (
	ErrIncompleteDependencies = errors.New("import provider dependencies are incomplete")
	ErrEntryLimit             = importport.ErrEntryLimit
	ErrPaginationLimit        = importport.ErrPaginationLimit
	ErrInvalidInput           = importport.ErrInvalidInput
)

// Service fetches and normalizes entries from supported external trackers.
type Service struct {
	client          *http.Client
	defaultTimezone string
	now             func() time.Time
}

// New creates an importer using the process-owned HTTP client and default timezone.
func New(client *http.Client, defaultTimezone string, now func() time.Time) (*Service, error) {
	if depcheck.IsNil(client) || now == nil {
		return nil, ErrIncompleteDependencies
	}
	return &Service{client: client, defaultTimezone: defaultTimezone, now: now}, nil
}

// Fetch retrieves and normalizes entries from a configured provider.
func (s *Service) Fetch(ctx context.Context, request importport.ProviderRequest) ([]importport.ImportedEntry, error) {
	return fetchWithClient(s.client, ctx, request, s.defaultTimezone, s.now())
}

func fetchWithClient(client *http.Client, ctx context.Context, request importport.ProviderRequest, defaultTimezone string, now time.Time) ([]importport.ImportedEntry, error) {
	entries, err := fetchEntries(client, ctx, request, defaultTimezone, now)
	if err != nil {
		return nil, err
	}
	result := make([]importport.ImportedEntry, len(entries))
	for i, entry := range entries {
		result[i] = importport.ImportedEntry{ExternalID: entry.ExtID, Activity: entry.Activity, Start: entry.Start, End: entry.End, Note: entry.Note}
	}
	return result, nil
}

func requestJSON(ctx context.Context, client *http.Client, vendor string, mk func() (*http.Request, error), into any) (http.Header, error) {
	resp, err := httpretry.Do(ctx, client, []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}, vendor, mk)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.Header, httpjson.Decode(resp.Body, httpjson.MaxResponseBytes, into)
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

func loadZone(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return location
}

func fetchEntries(client *http.Client, ctx context.Context, request importport.ProviderRequest, defaultTimezone string, now time.Time) ([]importedEntry, error) {
	fromT, toT, err := parseImportRange(request.From, request.To, request.Timezone, defaultTimezone, now)
	if err != nil {
		return nil, err
	}
	switch request.Provider {
	case "toggl":
		return fetchTogglEntries(client, ctx, request.Secret, fromT, toT, now.In(fromT.Location()))
	case "harvest":
		return fetchHarvestEntries(client, ctx, request.Secret, request.Extra, fromT, toT)
	case "clockify":
		return fetchClockifyEntries(client, ctx, request.Secret, request.Extra, fromT, toT)
	default:
		return nil, fmt.Errorf("%w: unknown provider %q", ErrInvalidInput, request.Provider)
	}
}

func parseImportRange(from, to, tz, defaultTimezone string, now time.Time) (time.Time, time.Time, error) {
	loc := loadZone(defaultTimezone)
	if loc == nil {
		loc = time.Local
	}
	if l, err := time.LoadLocation(tz); tz != "" && err == nil {
		loc = l
	}
	now = now.In(loc)
	fromT := now.AddDate(0, 0, -30)
	toT := now
	if from != "" {
		t, err := time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return fromT, toT, fmt.Errorf("%w: bad from date", ErrInvalidInput)
		}
		fromT = t
	}
	if to != "" {
		t, err := time.ParseInLocation("2006-01-02", to, loc)
		if err != nil {
			return fromT, toT, fmt.Errorf("%w: bad to date", ErrInvalidInput)
		}
		toT = t.AddDate(0, 0, 1)
	}
	if !toT.After(fromT) {
		return fromT, toT, fmt.Errorf("%w: end date must be after start date", ErrInvalidInput)
	}
	return fromT, toT, nil
}
