package calendarsync

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/calendarport"
)

// Microsoft calls Microsoft Graph for the member's own calendar. Times and bodies
// are requested in UTC and plain text, so the sync reads one format only.
// Authorization code with PKCE: https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow
// Calendar view and events: https://learn.microsoft.com/en-us/graph/api/calendar-list-calendarview
type Microsoft struct{ base }

const microsoftScope = "offline_access Calendars.ReadWrite"

// graphTimeLayout is the dateTime form Graph uses; it accepts the seven-digit
// fraction that comes back with it.
const graphTimeLayout = "2006-01-02T15:04:05"

var microsoftDefaults = Endpoints{
	Authorization: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
	Token:         "https://login.microsoftonline.com/common/oauth2/v2.0/token",
	API:           "https://graph.microsoft.com/v1.0",
}

// NewMicrosoft builds the Microsoft adapter with the same refusals as NewGoogle.
func NewMicrosoft(cfg Config) (*Microsoft, error) {
	b, err := newBase("microsoft", cfg, microsoftDefaults)
	if err != nil {
		return nil, err
	}
	return &Microsoft{base: b}, nil
}

type graphTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type graphEvent struct {
	ID          string    `json:"id"`
	ChangeKey   string    `json:"changeKey"`
	Subject     string    `json:"subject"`
	IsAllDay    bool      `json:"isAllDay"`
	IsCancelled bool      `json:"isCancelled"`
	Start       graphTime `json:"start"`
	End         graphTime `json:"end"`
	Location    struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Body struct {
		Content string `json:"content"`
	} `json:"body"`
}

// graphWrite is the body of a create or a patch.
type graphWrite struct {
	Subject string `json:"subject"`
	Body    struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Start graphTime `json:"start"`
	End   graphTime `json:"end"`
}

func (m *Microsoft) AuthorizationURL(request calendarport.AuthorizationRequest) string {
	query := url.Values{
		"client_id":             {m.creds.ClientID},
		"redirect_uri":          {request.RedirectURI},
		"response_type":         {"code"},
		"response_mode":         {"query"},
		"scope":                 {microsoftScope},
		"state":                 {request.State},
		"code_challenge":        {request.Challenge},
		"code_challenge_method": {"S256"},
	}
	return m.endpoints.Authorization + "?" + query.Encode()
}

func (m *Microsoft) Exchange(ctx context.Context, code, verifier, redirectURI string) (calendarport.Tokens, error) {
	return m.postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
		"scope":         {microsoftScope},
	}, false)
}

func (m *Microsoft) Refresh(ctx context.Context, refreshToken string) (calendarport.Tokens, error) {
	return m.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {microsoftScope},
	}, true)
}

// List reads calendarView, which expands recurring events into instances. Paging
// follows @odata.nextLink only while it stays on the Graph origin, so the access
// token is never sent to any other host.
func (m *Microsoft) List(ctx context.Context, accessToken string, window calendarport.Window) ([]calendarport.RemoteEvent, error) {
	query := url.Values{
		"startDateTime": {window.From.UTC().Format(time.RFC3339)},
		"endDateTime":   {window.To.UTC().Format(time.RFC3339)},
		"$top":          {"100"},
		"$select":       {"id,changeKey,subject,start,end,location,body,isAllDay,isCancelled"},
	}
	header := http.Header{"Prefer": {`outlook.timezone="UTC", outlook.body-content-type="text"`}}
	endpoint := m.endpoints.API + "/me/calendarView?" + query.Encode()
	var out []calendarport.RemoteEvent
	for range maxPages {
		var page struct {
			Value    []graphEvent `json:"value"`
			NextLink string       `json:"@odata.nextLink"`
		}
		if err := m.send(ctx, http.MethodGet, endpoint, accessToken, header, nil, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Value {
			if event, ok := graphRemote(item); ok {
				out = append(out, event)
			}
		}
		if page.NextLink == "" {
			return out, nil
		}
		if !strings.HasPrefix(page.NextLink, m.endpoints.API+"/") {
			return nil, fmt.Errorf("microsoft returned a paging link outside the Graph API")
		}
		endpoint = page.NextLink
	}
	return nil, errTooManyPages
}

// graphRemote maps a listed event. Cancelled and all-day events are left out, as
// they are for Google.
func graphRemote(item graphEvent) (calendarport.RemoteEvent, bool) {
	if item.IsCancelled || item.IsAllDay {
		return calendarport.RemoteEvent{}, false
	}
	starts, err := time.ParseInLocation(graphTimeLayout, item.Start.DateTime, time.UTC)
	if err != nil {
		return calendarport.RemoteEvent{}, false
	}
	ends, err := time.ParseInLocation(graphTimeLayout, item.End.DateTime, time.UTC)
	if err != nil {
		return calendarport.RemoteEvent{}, false
	}
	return calendarport.RemoteEvent{ID: item.ID, Etag: item.ChangeKey, Event: calendarport.Event{
		Title: item.Subject, Location: item.Location.DisplayName, Notes: item.Body.Content, StartsAt: starts, EndsAt: ends,
	}}, true
}

func graphBodyOf(event calendarport.Event) graphWrite {
	var body graphWrite
	body.Subject = event.Title
	body.Body.ContentType = "text"
	body.Body.Content = event.Notes
	body.Location.DisplayName = event.Location
	body.Start = graphTime{DateTime: event.StartsAt.UTC().Format(graphTimeLayout), TimeZone: "UTC"}
	body.End = graphTime{DateTime: event.EndsAt.UTC().Format(graphTimeLayout), TimeZone: "UTC"}
	return body
}

func (m *Microsoft) Create(ctx context.Context, accessToken string, event calendarport.Event) (calendarport.RemoteEvent, error) {
	var created graphEvent
	if err := m.send(ctx, http.MethodPost, m.endpoints.API+"/me/events", accessToken, nil, graphBodyOf(event), &created); err != nil {
		return calendarport.RemoteEvent{}, err
	}
	return calendarport.RemoteEvent{ID: created.ID, Etag: created.ChangeKey, Event: event}, nil
}

func (m *Microsoft) Update(ctx context.Context, accessToken, id string, event calendarport.Event) (calendarport.RemoteEvent, error) {
	var updated graphEvent
	err := m.send(ctx, http.MethodPatch, m.endpoints.API+"/me/events/"+url.PathEscape(id), accessToken, nil, graphBodyOf(event), &updated)
	if isGone(err) {
		return calendarport.RemoteEvent{}, calendarport.ErrRemoteEventGone
	}
	if err != nil {
		return calendarport.RemoteEvent{}, err
	}
	return calendarport.RemoteEvent{ID: id, Etag: updated.ChangeKey, Event: event}, nil
}

func (m *Microsoft) Delete(ctx context.Context, accessToken, id string) error {
	err := m.send(ctx, http.MethodDelete, m.endpoints.API+"/me/events/"+url.PathEscape(id), accessToken, nil, nil, nil)
	if isGone(err) {
		return calendarport.ErrRemoteEventGone
	}
	return err
}
