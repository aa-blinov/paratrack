package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/calendarport"
)

// pushStore answers the three store reads and writes the push path makes;
// the pull-path methods of the embedded interface are never reached.
type pushStore struct {
	SyncStore
	candidates []appmodel.CalendarPushCandidate
	links      []appmodel.CalendarTeamLinkRequest
}

func (s *pushStore) CalendarPushCandidates(context.Context, appmodel.CalendarPushQuery) ([]appmodel.CalendarPushCandidate, error) {
	return s.candidates, nil
}

func (s *pushStore) CalendarTombstones(context.Context, appmodel.CalendarTombstoneRequest) ([]appmodel.CalendarTombstone, error) {
	return nil, nil
}

func (s *pushStore) SaveCalendarTeamLink(_ context.Context, request appmodel.CalendarTeamLinkRequest) error {
	s.links = append(s.links, request)
	return nil
}

type pushAdapter struct {
	calendarport.Adapter
	creates int
}

func (a *pushAdapter) Create(_ context.Context, _ string, event calendarport.Event) (calendarport.RemoteEvent, error) {
	a.creates++
	return calendarport.RemoteEvent{ID: "ext-created", Etag: "created-etag", Event: event}, nil
}

// A link write that failed after the provider copy appeared left the twin in
// the listing. The next pass adopts that copy instead of creating a second
// one, and the link finally names it.
func TestCalendarPushAdoptsAnUnlinkedTwinInsteadOfCreatingADuplicate(t *testing.T) {
	span := calendarport.Event{
		Title: "Созвон", StartsAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		EndsAt: time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC),
	}
	remote := []calendarport.RemoteEvent{{ID: "ext-9", Etag: "e9", Event: span}}
	store := &pushStore{candidates: []appmodel.CalendarPushCandidate{{
		EventID: 5,
		Event:   appmodel.CalendarEventFields{Title: span.Title, StartsAt: span.StartsAt, EndsAt: span.EndsAt},
	}}}
	adapter := &pushAdapter{}
	svc := &SyncService{store: store, logger: nopLogger{}}
	connection := appmodel.CalendarConnection{TeamID: 1, UserID: 2, Provider: calendarport.ProviderGoogle}
	window := calendarport.Window{From: span.StartsAt.Add(-time.Hour), To: span.EndsAt.Add(time.Hour)}

	if err := svc.push(context.Background(), adapter, "token", connection, remote, window, span.StartsAt); err != nil {
		t.Fatalf("push: %v", err)
	}
	if adapter.creates != 0 {
		t.Fatalf("the twin was in the listing, yet Create ran %d times", adapter.creates)
	}
	if len(store.links) != 1 || store.links[0].ExternalID != "ext-9" || store.links[0].EventID != 5 {
		t.Fatalf("links = %+v, want the twin adopted for the team event 5", store.links)
	}
}

// No twin in the listing means the copy truly does not exist yet: the push
// creates it and links the fresh external id.
func TestCalendarPushCreatesWhenNothingMatches(t *testing.T) {
	span := calendarport.Event{
		Title: "Созвон", StartsAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		EndsAt: time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC),
	}
	other := calendarport.Event{
		Title: "Другое", StartsAt: time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC),
		EndsAt: time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC),
	}
	remote := []calendarport.RemoteEvent{{ID: "ext-1", Etag: "e1", Event: other}}
	store := &pushStore{candidates: []appmodel.CalendarPushCandidate{{
		EventID: 5,
		Event:   appmodel.CalendarEventFields{Title: span.Title, StartsAt: span.StartsAt, EndsAt: span.EndsAt},
	}}}
	adapter := &pushAdapter{}
	svc := &SyncService{store: store, logger: nopLogger{}}
	connection := appmodel.CalendarConnection{TeamID: 1, UserID: 2, Provider: calendarport.ProviderGoogle}
	window := calendarport.Window{From: span.StartsAt.Add(-time.Hour), To: span.EndsAt.Add(time.Hour)}

	if err := svc.push(context.Background(), adapter, "token", connection, remote, window, span.StartsAt); err != nil {
		t.Fatalf("push: %v", err)
	}
	if adapter.creates != 1 {
		t.Fatalf("without a twin Create ran %d times, want exactly one", adapter.creates)
	}
	if len(store.links) != 1 || store.links[0].ExternalID != "ext-created" {
		t.Fatalf("links = %+v, want the created copy linked", store.links)
	}
}
