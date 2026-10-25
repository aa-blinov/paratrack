package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

// Calendar sync (CRM-19) connects a member's own Google or Outlook calendar to the
// workspace calendar. Each connection is one member in one workspace with one provider.
//
// What goes where. A provider event in the window (30 days back, 90 days ahead) becomes
// a personal event of the member, linked to the provider's ID; the member sees it and the
// managers see it, the rest of the team does not. A team event the member created with
// push on goes to the provider and is linked the same way.
//
// Conflicts. The provider wins for a pulled event: when its values change there, the
// local copy takes them at the next sync, and a local edit to a pulled event lasts only
// until then. The team wins for a pushed event: a provider-side edit is overwritten by
// the team's values at the next sync. A deletion follows the side that owns the event: a
// removed pulled event is removed here only when the provider dropped it inside the window
// (a listing that fails part way deletes nothing); a deleted pushed event is deleted at
// the provider.

// SyncStore is the persistence the sync needs. Every method is scoped to one member of
// one workspace, and the adapter enforces that scope in its statements.
type SyncStore interface {
	CalendarConnections(ctx context.Context, teamID, userID int64) ([]appmodel.CalendarConnection, error)
	CalendarCredentials(ctx context.Context, teamID, userID int64, provider string) (appmodel.CalendarCredentials, error)
	DueCalendarConnections(ctx context.Context, before time.Time) ([]appmodel.CalendarConnection, error)
	SaveCalendarConnection(ctx context.Context, request appmodel.CalendarConnectionSaveRequest) error
	RecordCalendarSync(ctx context.Context, record appmodel.CalendarSyncRecordRequest) error
	DeleteCalendarConnection(ctx context.Context, request appmodel.CalendarConnectionDeleteRequest) error
	SaveCalendarOAuthState(ctx context.Context, request appmodel.CalendarOAuthStateSaveRequest) error
	TakeCalendarOAuthState(ctx context.Context, request appmodel.CalendarOAuthStateTakeRequest) (appmodel.CalendarOAuthState, error)
	CalendarLinks(ctx context.Context, query appmodel.CalendarLinksQuery) ([]appmodel.CalendarLink, error)
	CalendarPushCandidates(ctx context.Context, query appmodel.CalendarPushQuery) ([]appmodel.CalendarPushCandidate, error)
	CalendarTombstones(ctx context.Context, query appmodel.CalendarTombstoneRequest) ([]appmodel.CalendarTombstone, error)
	DeleteCalendarTombstone(ctx context.Context, query appmodel.CalendarTombstoneRequest) error
	CreatePulledCalendarEvent(ctx context.Context, request appmodel.CalendarPulledEventRequest) (int64, error)
	UpdatePulledCalendarEvent(ctx context.Context, request appmodel.CalendarPulledEventRequest) error
	RemovePulledCalendarEvent(ctx context.Context, request appmodel.CalendarSyncRemovalRequest) error
	SaveCalendarTeamLink(ctx context.Context, request appmodel.CalendarTeamLinkRequest) error
}

// SyncDependencies are the ports of the sync. Adapters holds only the providers whose
// OAuth client is configured; an empty map turns the sync off.
type SyncDependencies struct {
	Store    SyncStore
	Adapters map[string]calendarport.Adapter
	Now      func() time.Time
	Logger   Logger
}

// Logger is the one logging call the scheduler needs.
type Logger interface {
	Printf(string, ...any)
}

// SyncService runs the connections of the calendar sync.
type SyncService struct {
	store    SyncStore
	adapters map[string]calendarport.Adapter
	now      func() time.Time
	logger   Logger
	// run serializes the syncs of this process, so an on-demand sync cannot create a
	// pushed event twice while the scheduler is doing the same connection.
	// ponytail: one lock for every connection; per-connection locks if syncs get slow.
	run sync.Mutex
}

const (
	// syncInterval is the cadence of one member's connection in the scheduler.
	syncInterval = 15 * time.Minute
	pullBack     = 30 * 24 * time.Hour
	pullAhead    = 90 * 24 * time.Hour
	// consentTTL bounds how long a browser may sit on the provider's consent screen.
	consentTTL = 10 * time.Minute
	// refreshMargin refreshes an access token before it expires mid-sync.
	refreshMargin = 5 * time.Minute
	// untitledEvent names a provider event that arrives without a title.
	untitledEvent = "Без названия"
)

// NewSync builds the calendar sync service.
func NewSync(deps SyncDependencies) (*SyncService, error) {
	if depcheck.IsNil(deps.Store) || deps.Now == nil || depcheck.IsNil(deps.Logger) {
		return nil, ErrIncompleteDependencies
	}
	adapters := make(map[string]calendarport.Adapter, len(deps.Adapters))
	for name, adapter := range deps.Adapters {
		if !depcheck.IsNil(adapter) {
			adapters[name] = adapter
		}
	}
	return &SyncService{store: deps.Store, adapters: adapters, now: deps.Now, logger: deps.Logger}, nil
}

// Providers lists the providers that can connect on this server, in a fixed order.
func (s *SyncService) Providers() []string {
	var out []string
	for _, name := range []string{calendarport.ProviderGoogle, calendarport.ProviderMicrosoft} {
		if _, ok := s.adapters[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// Connections lists a member's connections in the workspace, without their tokens.
func (s *SyncService) Connections(ctx context.Context, teamID, userID int64) ([]appmodel.CalendarConnection, error) {
	if teamID <= 0 || userID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.CalendarConnections(ctx, teamID, userID)
}

// BeginConnect starts a consent. It returns the provider's authorization URL and keeps
// the state and the verifier bound to this member and this browser session.
func (s *SyncService) BeginConnect(ctx context.Context, request appmodel.CalendarConnectRequest) (string, error) {
	if request.TeamID <= 0 || request.UserID <= 0 || request.SessionToken == "" {
		return "", model.ErrCalendarStateInvalid
	}
	adapter, ok := s.adapters[request.Provider]
	if !ok {
		return "", model.ErrCalendarSyncDisabled
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(verifier))
	if err := s.store.SaveCalendarOAuthState(ctx, appmodel.CalendarOAuthStateSaveRequest{
		TeamID: request.TeamID, UserID: request.UserID, Provider: request.Provider,
		State: state, Verifier: verifier, SessionToken: request.SessionToken, ExpiresAt: s.now().UTC().Add(consentTTL),
	}); err != nil {
		return "", err
	}
	return adapter.AuthorizationURL(calendarport.AuthorizationRequest{
		RedirectURI: request.RedirectURI, State: state, Challenge: base64.RawURLEncoding.EncodeToString(digest[:]),
	}), nil
}

// CompleteConnect exchanges the provider's code and stores the tokens. A consent is
// accepted once, only for the member, workspace and provider that started it. The first
// sync runs at once; its failure is recorded on the connection, not returned, so the
// connection stays and the page shows why it has no events yet.
func (s *SyncService) CompleteConnect(ctx context.Context, request appmodel.CalendarConnectCallbackRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 {
		return model.ErrCalendarStateInvalid
	}
	adapter, ok := s.adapters[request.Provider]
	if !ok {
		return model.ErrCalendarSyncDisabled
	}
	if request.Code == "" {
		return model.ErrCalendarStateInvalid
	}
	state, err := s.store.TakeCalendarOAuthState(ctx, appmodel.CalendarOAuthStateTakeRequest{State: request.State, SessionToken: request.SessionToken})
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrCalendarStateInvalid
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if state.TeamID != request.TeamID || state.UserID != request.UserID || state.Provider != request.Provider || !now.Before(state.ExpiresAt) {
		return model.ErrCalendarStateInvalid
	}
	tokens, err := adapter.Exchange(ctx, request.Code, state.Verifier, request.RedirectURI)
	if err != nil {
		s.logger.Printf("calendar sync: %s code exchange for team %d: %v", request.Provider, request.TeamID, err)
		return model.ErrCalendarProviderFailure
	}
	s.run.Lock()
	err = s.store.SaveCalendarConnection(ctx, appmodel.CalendarConnectionSaveRequest{
		TeamID: request.TeamID, UserID: request.UserID, Provider: request.Provider, Tokens: toAppTokens(tokens), Now: now,
	})
	s.run.Unlock()
	if err != nil {
		return err
	}
	if err := s.Sync(ctx, request.TeamID, request.UserID, request.Provider); err != nil {
		s.logger.Printf("calendar sync: first sync of %s for team %d: %v", request.Provider, request.TeamID, err)
	}
	return nil
}

// Disconnect removes the member's tokens, links and tombstones for the provider. The
// events stay in the workspace as ordinary events.
func (s *SyncService) Disconnect(ctx context.Context, teamID, userID int64, provider string) error {
	if teamID <= 0 || userID <= 0 {
		return model.ErrNotFound
	}
	s.run.Lock()
	defer s.run.Unlock()
	return s.store.DeleteCalendarConnection(ctx, appmodel.CalendarConnectionDeleteRequest{TeamID: teamID, UserID: userID, Provider: provider})
}

// SyncMine runs every connection of the member in the workspace now. It refuses a member
// who has none, and returns the connections with the outcome of this run.
func (s *SyncService) SyncMine(ctx context.Context, teamID, userID int64) ([]appmodel.CalendarConnection, error) {
	if teamID <= 0 || userID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	connections, err := s.store.CalendarConnections(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}
	if len(connections) == 0 {
		return nil, model.ErrCalendarNotConnected
	}
	for _, connection := range connections {
		if _, ok := s.adapters[connection.Provider]; !ok {
			continue
		}
		if err := s.Sync(ctx, teamID, userID, connection.Provider); err != nil && !errors.Is(err, model.ErrCalendarSyncDisabled) {
			s.logger.Printf("calendar sync: %s for team %d: %v", connection.Provider, teamID, err)
		}
	}
	return s.store.CalendarConnections(ctx, teamID, userID)
}

// SyncDue runs every connection whose last attempt is older than the cadence. It is the
// scheduler's pass, and it returns how many connections it ran.
func (s *SyncService) SyncDue(ctx context.Context) int {
	due, err := s.store.DueCalendarConnections(ctx, s.now().UTC().Add(-syncInterval))
	if err != nil {
		s.logger.Printf("calendar sync: list due connections: %v", err)
		return 0
	}
	ran := 0
	for _, connection := range due {
		if ctx.Err() != nil {
			break
		}
		if _, ok := s.adapters[connection.Provider]; !ok {
			continue
		}
		if err := s.Sync(ctx, connection.TeamID, connection.UserID, connection.Provider); err != nil {
			s.logger.Printf("calendar sync: %s for team %d member %d: %v", connection.Provider, connection.TeamID, connection.UserID, err)
		}
		ran++
	}
	return ran
}

// Run is the scheduler loop: one pass now, then one per interval until ctx ends.
func (s *SyncService) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.SyncDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sync runs one connection and records the outcome. A failure leaves the stored events
// as they were: the listing is read whole before anything is deleted, and each write is
// its own statement, so a failure part way leaves only finished work behind.
func (s *SyncService) Sync(ctx context.Context, teamID, userID int64, provider string) error {
	if teamID <= 0 || userID <= 0 {
		return appmodel.ErrInvalidWorkspace
	}
	s.run.Lock()
	defer s.run.Unlock()
	adapter, ok := s.adapters[provider]
	if !ok {
		return model.ErrCalendarSyncDisabled
	}
	creds, err := s.store.CalendarCredentials(ctx, teamID, userID, provider)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	syncErr := s.runSync(ctx, adapter, creds, now)
	if err := s.store.RecordCalendarSync(ctx, appmodel.CalendarSyncRecordRequest{
		TeamID: teamID, UserID: userID, Provider: provider, At: now, Error: syncFailureCode(syncErr),
	}); err != nil && syncErr == nil {
		return err
	}
	return syncErr
}

func (s *SyncService) runSync(ctx context.Context, adapter calendarport.Adapter, creds appmodel.CalendarCredentials, now time.Time) error {
	connection := creds.Connection
	token, err := s.accessToken(ctx, adapter, creds, now)
	if err != nil {
		return err
	}
	window := calendarport.Window{From: now.Add(-pullBack), To: now.Add(pullAhead)}
	remote, err := adapter.List(ctx, token, window)
	if err != nil {
		return err
	}
	if err := s.pull(ctx, connection, remote, window, now); err != nil {
		return err
	}
	return s.push(ctx, adapter, token, connection, remote, window, now)
}

// accessToken returns a usable access token, refreshing and saving the new one when the
// stored token is near expiry.
func (s *SyncService) accessToken(ctx context.Context, adapter calendarport.Adapter, creds appmodel.CalendarCredentials, now time.Time) (string, error) {
	connection := creds.Connection
	if creds.Tokens.AccessToken != "" && creds.Tokens.ExpiresAt.After(now.Add(refreshMargin)) {
		return creds.Tokens.AccessToken, nil
	}
	if creds.Tokens.RefreshToken == "" {
		return "", calendarport.ErrTokenRejected
	}
	fresh, err := adapter.Refresh(ctx, creds.Tokens.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := s.store.SaveCalendarConnection(ctx, appmodel.CalendarConnectionSaveRequest{
		TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider, Tokens: toAppTokens(fresh), Now: now,
	}); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// pull brings the provider's events into the workspace. The provider wins: a changed
// pulled event takes the provider's values, and a pulled event the listing no longer
// holds inside the window is removed.
func (s *SyncService) pull(ctx context.Context, connection appmodel.CalendarConnection, remote []calendarport.RemoteEvent, window calendarport.Window, now time.Time) error {
	query := appmodel.CalendarLinksQuery{TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider}
	links, err := s.store.CalendarLinks(ctx, query)
	if err != nil {
		return err
	}
	tombstones, err := s.store.CalendarTombstones(ctx, appmodel.CalendarTombstoneRequest{TeamID: query.TeamID, UserID: query.UserID, Provider: query.Provider})
	if err != nil {
		return err
	}
	linkByExternal := make(map[string]appmodel.CalendarLink, len(links))
	for _, link := range links {
		linkByExternal[link.ExternalID] = link
	}
	// A tombstone of either kind keeps the listed event out of the pull: an ignored one
	// stays deleted here, and a queued delete is about to leave the provider.
	ignored := make(map[string]bool, len(tombstones))
	for _, tombstone := range tombstones {
		ignored[tombstone.ExternalID] = true
	}
	listed := make(map[string]bool, len(remote))
	for _, event := range remote {
		listed[event.ID] = true
		if ignored[event.ID] {
			continue
		}
		fields, ok := importedFields(event.Event)
		if !ok {
			continue
		}
		link, linked := linkByExternal[event.ID]
		switch {
		case linked && link.Origin == appmodel.CalendarOriginTeam:
			// The team wins for a pushed event; the push step compares its etag.
			continue
		case linked:
			if event.Etag == link.Etag && sameFields(link.Event, fields) {
				continue
			}
			if err := s.store.UpdatePulledCalendarEvent(ctx, appmodel.CalendarPulledEventRequest{
				TeamID: query.TeamID, UserID: query.UserID, Provider: query.Provider, EventID: link.EventID,
				ExternalID: event.ID, Etag: event.Etag, Event: fields, SyncedAt: now,
			}); err != nil {
				return err
			}
		default:
			if _, err := s.store.CreatePulledCalendarEvent(ctx, appmodel.CalendarPulledEventRequest{
				TeamID: query.TeamID, UserID: query.UserID, Provider: query.Provider,
				ExternalID: event.ID, Etag: event.Etag, Event: fields, SyncedAt: now,
			}); err != nil {
				return err
			}
		}
	}
	for _, tombstone := range tombstones {
		if tombstone.Kind == appmodel.CalendarTombstoneIgnore && !listed[tombstone.ExternalID] {
			if err := s.store.DeleteCalendarTombstone(ctx, appmodel.CalendarTombstoneRequest{TeamID: query.TeamID, UserID: query.UserID, Provider: query.Provider, ExternalID: tombstone.ExternalID}); err != nil {
				return err
			}
		}
	}
	for _, link := range links {
		if link.Origin != appmodel.CalendarOriginProvider || listed[link.ExternalID] || !overlaps(link.Event.StartsAt, link.Event.EndsAt, window) {
			continue
		}
		if err := s.store.RemovePulledCalendarEvent(ctx, appmodel.CalendarSyncRemovalRequest{TeamID: query.TeamID, UserID: query.UserID, Provider: query.Provider, EventID: link.EventID}); err != nil {
			return err
		}
	}
	return nil
}

// push sends the member's events with push on, and the deletions the team queued. The
// team wins: a pushed event is written again when the team changed it, or when the
// provider's copy changed or went missing.
func (s *SyncService) push(ctx context.Context, adapter calendarport.Adapter, token string, connection appmodel.CalendarConnection, remote []calendarport.RemoteEvent, window calendarport.Window, now time.Time) error {
	present := make(map[string]calendarport.RemoteEvent, len(remote))
	for _, event := range remote {
		present[event.ID] = event
	}
	candidates, err := s.store.CalendarPushCandidates(ctx, appmodel.CalendarPushQuery{
		TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider, From: window.From, To: window.To,
	})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		event := toProviderEvent(candidate.Event)
		var written calendarport.RemoteEvent
		if !candidate.Linked {
			written, err = adapter.Create(ctx, token, event)
		} else if remoteEvent, ok := present[candidate.Link.ExternalID]; !ok {
			written, err = adapter.Create(ctx, token, event)
		} else if remoteEvent.Etag == candidate.Link.Etag && !candidate.UpdatedAt.After(candidate.Link.SyncedAt) {
			continue
		} else {
			written, err = adapter.Update(ctx, token, candidate.Link.ExternalID, event)
			if errors.Is(err, calendarport.ErrRemoteEventGone) {
				written, err = adapter.Create(ctx, token, event)
			}
		}
		if err != nil {
			return err
		}
		if err := s.store.SaveCalendarTeamLink(ctx, appmodel.CalendarTeamLinkRequest{
			TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider, EventID: candidate.EventID,
			ExternalID: written.ID, Etag: written.Etag, SyncedAt: now,
		}); err != nil {
			return err
		}
	}
	tombstones, err := s.store.CalendarTombstones(ctx, appmodel.CalendarTombstoneRequest{TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider})
	if err != nil {
		return err
	}
	for _, tombstone := range tombstones {
		if tombstone.Kind != appmodel.CalendarTombstoneDelete {
			continue
		}
		if err := adapter.Delete(ctx, token, tombstone.ExternalID); err != nil && !errors.Is(err, calendarport.ErrRemoteEventGone) {
			return err
		}
		if err := s.store.DeleteCalendarTombstone(ctx, appmodel.CalendarTombstoneRequest{TeamID: connection.TeamID, UserID: connection.UserID, Provider: connection.Provider, ExternalID: tombstone.ExternalID}); err != nil {
			return err
		}
	}
	return nil
}

// importedFields maps a provider event to the stored shape. An event that cannot be
// stored (no end after its start) is skipped, and an over-long text is cut to the limit
// the form enforces.
func importedFields(event calendarport.Event) (appmodel.CalendarEventFields, bool) {
	fields := appmodel.CalendarEventFields{
		Title: clip(event.Title, maxTitleLength), Location: clip(event.Location, maxLocationLength),
		Notes: clip(event.Notes, maxNotesLength), StartsAt: event.StartsAt.UTC(), EndsAt: event.EndsAt.UTC(),
	}
	if fields.Title == "" {
		fields.Title = untitledEvent
	}
	return fields, fields.EndsAt.After(fields.StartsAt)
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut])
}

func sameFields(a, b appmodel.CalendarEventFields) bool {
	return a.Title == b.Title && a.Location == b.Location && a.Notes == b.Notes && a.StartsAt.Equal(b.StartsAt) && a.EndsAt.Equal(b.EndsAt)
}

func overlaps(starts, ends time.Time, window calendarport.Window) bool {
	return starts.Before(window.To) && ends.After(window.From)
}

func toProviderEvent(fields appmodel.CalendarEventFields) calendarport.Event {
	return calendarport.Event{Title: fields.Title, Location: fields.Location, Notes: fields.Notes, StartsAt: fields.StartsAt, EndsAt: fields.EndsAt}
}

func toAppTokens(tokens calendarport.Tokens) appmodel.CalendarTokens {
	return appmodel.CalendarTokens{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: tokens.ExpiresAt}
}

// syncFailureCode is the text the connection keeps for the page. The page translates the
// codes; a provider's refusal keeps its vendor and status, which name no account data.
func syncFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, calendarport.ErrTokenRejected):
		return "token_rejected"
	case errors.Is(err, calendarport.ErrUnavailable):
		return "unreachable"
	}
	var status *providerstatus.Error
	if errors.As(err, &status) {
		return status.Error()
	}
	return "internal"
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate calendar token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
