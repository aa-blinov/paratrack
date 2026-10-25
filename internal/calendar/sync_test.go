package calendar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

func TestCalendarSyncImportedEventsKeepOnlyStorableFields(t *testing.T) {
	start := time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC)
	if _, ok := importedFields(calendarport.Event{Title: "Тест", StartsAt: start, EndsAt: start}); ok {
		t.Fatal("an event that ends when it starts was imported")
	}
	fields, ok := importedFields(calendarport.Event{StartsAt: start, EndsAt: start.Add(time.Hour)})
	if !ok || fields.Title != untitledEvent {
		t.Fatalf("untitled event = %+v, %v; want the placeholder title", fields, ok)
	}
	long := strings.Repeat("я", maxTitleLength)
	fields, _ = importedFields(calendarport.Event{Title: long, StartsAt: start, EndsAt: start.Add(time.Hour)})
	if len(fields.Title) > maxTitleLength || !utf8.ValidString(fields.Title) {
		t.Fatalf("title of %d bytes was not cut to a valid string within %d bytes", len(fields.Title), maxTitleLength)
	}
}

func TestCalendarSyncFailureCodesNameTheCauseWithoutAccountData(t *testing.T) {
	cases := map[string]error{
		"token_rejected": calendarport.ErrTokenRejected,
		"unreachable":    calendarport.ErrUnavailable,
		"google 500":     &providerstatus.Error{Vendor: "google", StatusCode: http.StatusInternalServerError},
		"internal":       errors.New("database closed"),
		"":               nil,
	}
	for want, err := range cases {
		if got := syncFailureCode(err); got != want {
			t.Fatalf("syncFailureCode(%v) = %q, want %q", err, got, want)
		}
	}
}

type stubAdapter struct{ calendarport.Adapter }

func TestCalendarSyncOffWithoutAnOAuthClient(t *testing.T) {
	store := &stubSyncStore{}
	svc, err := NewSync(SyncDependencies{Store: store, Adapters: map[string]calendarport.Adapter{}, Now: time.Now, Logger: nopLogger{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.Providers(); len(got) != 0 {
		t.Fatalf("providers = %v with no OAuth client configured", got)
	}
	_, err = svc.BeginConnect(context.Background(), appmodel.CalendarConnectRequest{TeamID: 1, UserID: 2, Provider: calendarport.ProviderGoogle, SessionToken: "s"})
	if !errors.Is(err, model.ErrCalendarSyncDisabled) {
		t.Fatalf("connect with the sync off = %v, want ErrCalendarSyncDisabled", err)
	}
	if err := svc.Sync(context.Background(), 1, 2, calendarport.ProviderGoogle); !errors.Is(err, model.ErrCalendarSyncDisabled) {
		t.Fatalf("sync with the sync off = %v, want ErrCalendarSyncDisabled", err)
	}
}

func TestCalendarSyncProvidersKeepTheFixedOrder(t *testing.T) {
	svc, err := NewSync(SyncDependencies{
		Store: &stubSyncStore{}, Now: time.Now, Logger: nopLogger{},
		Adapters: map[string]calendarport.Adapter{calendarport.ProviderMicrosoft: stubAdapter{}, calendarport.ProviderGoogle: stubAdapter{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := svc.Providers()
	if len(got) != 2 || got[0] != calendarport.ProviderGoogle || got[1] != calendarport.ProviderMicrosoft {
		t.Fatalf("providers = %v, want google then microsoft", got)
	}
}

func TestCalendarSyncRejectsIncompleteDependencies(t *testing.T) {
	if _, err := NewSync(SyncDependencies{Now: time.Now, Logger: nopLogger{}}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("NewSync without a store = %v, want ErrIncompleteDependencies", err)
	}
}

// stubSyncStore satisfies SyncStore for the constructor and refusal tests; none of its
// methods is reached on those paths.
type stubSyncStore struct{ SyncStore }

type nopLogger struct{}

func (nopLogger) Printf(string, ...any) {}
