// Package calendarport defines the outbound calendar capability the calendar
// sync workflow uses. Provider adapters implement Adapter; the workflow never
// sees an HTTP request or a provider's JSON.
package calendarport

import (
	"context"
	"errors"
	"time"
)

// Provider names are the keys of the adapter map and the values stored in
// calendar_connections.provider.
const (
	ProviderGoogle    = "google"
	ProviderMicrosoft = "microsoft"
)

// ErrTokenRejected means the provider refused the token itself. The connection
// cannot sync until the member connects again.
var ErrTokenRejected = errors.New("the provider rejected the token")

// ErrUnavailable means the provider could not be reached at all.
var ErrUnavailable = errors.New("the provider could not be reached")

// ErrRemoteEventGone means the provider holds no event with that ID.
var ErrRemoteEventGone = errors.New("the provider has no such event")

// Credentials is the OAuth client of one provider, read from the process config.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// AuthorizationRequest carries what the consent screen needs. Challenge is the
// PKCE S256 challenge; the verifier stays on the server.
type AuthorizationRequest struct {
	RedirectURI string
	State       string
	Challenge   string
}

// Tokens is the provider's answer to a code exchange or a refresh. A refresh
// that returns no refresh token keeps the stored one.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Window is the span of provider events one sync reads: [From, To).
type Window struct {
	From time.Time
	To   time.Time
}

// Event is the part of a calendar event both sides share. Times are instants.
type Event struct {
	Title    string
	Location string
	Notes    string
	StartsAt time.Time
	EndsAt   time.Time
}

// RemoteEvent is an event as the provider holds it. Etag changes whenever the
// provider changes the event, so it tells a changed event from an unchanged one.
type RemoteEvent struct {
	ID   string
	Etag string
	Event
}

// Adapter talks to one calendar provider. Every call takes the access token the
// workflow holds; an adapter keeps no token of its own.
type Adapter interface {
	AuthorizationURL(AuthorizationRequest) string
	Exchange(ctx context.Context, code, verifier, redirectURI string) (Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (Tokens, error)
	// List returns the timed, non-cancelled events in the window. A failure on any
	// page fails the whole list, so the caller never mistakes a partial answer for
	// a deletion.
	List(ctx context.Context, accessToken string, window Window) ([]RemoteEvent, error)
	Create(ctx context.Context, accessToken string, event Event) (RemoteEvent, error)
	// Update returns ErrRemoteEventGone when the provider no longer has the event.
	Update(ctx context.Context, accessToken, id string, event Event) (RemoteEvent, error)
	// Delete returns ErrRemoteEventGone when the event is already gone.
	Delete(ctx context.Context, accessToken, id string) error
}
