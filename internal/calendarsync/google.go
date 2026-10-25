package calendarsync

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/aa-blinov/paratrack/internal/calendarport"
)

// Google calls the Google Calendar API v3 on the member's primary calendar.
// Authorization code with PKCE and offline access for a refresh token:
// https://developers.google.com/identity/protocols/oauth2/web-server
// Events: https://developers.google.com/workspace/calendar/api/v3/reference/events
type Google struct{ base }

const googleScope = "https://www.googleapis.com/auth/calendar.events"

var googleDefaults = Endpoints{
	Authorization: "https://accounts.google.com/o/oauth2/v2/auth",
	Token:         "https://oauth2.googleapis.com/token",
	API:           "https://www.googleapis.com/calendar/v3",
}

// NewGoogle builds the Google adapter. It refuses a config without an HTTP client,
// a clock or an OAuth client, so a half-configured adapter never starts.
func NewGoogle(cfg Config) (*Google, error) {
	b, err := newBase("google", cfg, googleDefaults)
	if err != nil {
		return nil, err
	}
	return &Google{base: b}, nil
}

// googleEvent is the part of an event resource the sync reads and writes.
type googleEvent struct {
	ID          string `json:"id"`
	Etag        string `json:"etag"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Start       struct {
		DateTime string `json:"dateTime"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
	} `json:"end"`
}

// googleWrite is the body of an insert or a patch. Google assigns the ID on insert.
type googleWrite struct {
	Summary     string `json:"summary"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Start       struct {
		DateTime string `json:"dateTime"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
	} `json:"end"`
}

func (g *Google) AuthorizationURL(request calendarport.AuthorizationRequest) string {
	query := url.Values{
		"client_id":             {g.creds.ClientID},
		"redirect_uri":          {request.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {googleScope},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {request.State},
		"code_challenge":        {request.Challenge},
		"code_challenge_method": {"S256"},
	}
	return g.endpoints.Authorization + "?" + query.Encode()
}

func (g *Google) Exchange(ctx context.Context, code, verifier, redirectURI string) (calendarport.Tokens, error) {
	return g.postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}, false)
}

func (g *Google) Refresh(ctx context.Context, refreshToken string) (calendarport.Tokens, error) {
	return g.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, true)
}

// List pages through events.list with singleEvents, so recurring events arrive
// as their own instances, each with its own ID.
func (g *Google) List(ctx context.Context, accessToken string, window calendarport.Window) ([]calendarport.RemoteEvent, error) {
	var out []calendarport.RemoteEvent
	pageToken := ""
	for range maxPages {
		query := url.Values{
			"timeMin":      {window.From.UTC().Format(time.RFC3339)},
			"timeMax":      {window.To.UTC().Format(time.RFC3339)},
			"singleEvents": {"true"},
			"maxResults":   {"250"},
		}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		var page struct {
			Items         []googleEvent `json:"items"`
			NextPageToken string        `json:"nextPageToken"`
		}
		if err := g.send(ctx, http.MethodGet, g.endpoints.API+"/calendars/primary/events?"+query.Encode(), accessToken, nil, nil, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if event, ok := googleRemote(item); ok {
				out = append(out, event)
			}
		}
		if page.NextPageToken == "" {
			return out, nil
		}
		pageToken = page.NextPageToken
	}
	return nil, errTooManyPages
}

// googleRemote maps a listed event. Cancelled events and all-day events (no
// dateTime) are left out: an all-day event has no time of day to show.
func googleRemote(item googleEvent) (calendarport.RemoteEvent, bool) {
	if item.Status == "cancelled" {
		return calendarport.RemoteEvent{}, false
	}
	starts, err := time.Parse(time.RFC3339, item.Start.DateTime)
	if err != nil {
		return calendarport.RemoteEvent{}, false
	}
	ends, err := time.Parse(time.RFC3339, item.End.DateTime)
	if err != nil {
		return calendarport.RemoteEvent{}, false
	}
	return calendarport.RemoteEvent{ID: item.ID, Etag: item.Etag, Event: calendarport.Event{
		Title: item.Summary, Location: item.Location, Notes: item.Description, StartsAt: starts, EndsAt: ends,
	}}, true
}

func googleBody(event calendarport.Event) googleWrite {
	var body googleWrite
	body.Summary = event.Title
	body.Location = event.Location
	body.Description = event.Notes
	body.Start.DateTime = event.StartsAt.UTC().Format(time.RFC3339)
	body.End.DateTime = event.EndsAt.UTC().Format(time.RFC3339)
	return body
}

func (g *Google) Create(ctx context.Context, accessToken string, event calendarport.Event) (calendarport.RemoteEvent, error) {
	var created googleEvent
	if err := g.send(ctx, http.MethodPost, g.endpoints.API+"/calendars/primary/events", accessToken, nil, googleBody(event), &created); err != nil {
		return calendarport.RemoteEvent{}, err
	}
	return calendarport.RemoteEvent{ID: created.ID, Etag: created.Etag, Event: event}, nil
}

func (g *Google) Update(ctx context.Context, accessToken, id string, event calendarport.Event) (calendarport.RemoteEvent, error) {
	var updated googleEvent
	err := g.send(ctx, http.MethodPatch, g.endpoints.API+"/calendars/primary/events/"+url.PathEscape(id), accessToken, nil, googleBody(event), &updated)
	if isGone(err) {
		return calendarport.RemoteEvent{}, calendarport.ErrRemoteEventGone
	}
	if err != nil {
		return calendarport.RemoteEvent{}, err
	}
	return calendarport.RemoteEvent{ID: id, Etag: updated.Etag, Event: event}, nil
}

func (g *Google) Delete(ctx context.Context, accessToken, id string) error {
	err := g.send(ctx, http.MethodDelete, g.endpoints.API+"/calendars/primary/events/"+url.PathEscape(id), accessToken, nil, nil, nil)
	if isGone(err) {
		return calendarport.ErrRemoteEventGone
	}
	return err
}
