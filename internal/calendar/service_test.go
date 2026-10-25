package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type fakeStore struct {
	Store
	listed int
}

func (f *fakeStore) ListCalendarEvents(context.Context, appmodel.CalendarEventListQuery) ([]model.CalendarEvent, error) {
	f.listed++
	return nil, nil
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var fe appmodel.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("error = %v, want a field error on %q", err, field)
	}
}

func TestCreateRefusesBlankTitleAndMissingTimes(t *testing.T) {
	svc, err := New(Dependencies{Store: &fakeStore{}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	if _, err := svc.Create(context.Background(), appmodel.CalendarEventCreateRequest{TeamID: 1, Title: " ", StartsAt: start, EndsAt: start}); err == nil {
		t.Fatal("blank title was accepted")
	} else {
		wantField(t, err, "event title")
	}
	if _, err := svc.Create(context.Background(), appmodel.CalendarEventCreateRequest{TeamID: 1, Title: "Планёрка", StartsAt: start}); err == nil {
		t.Fatal("event without an end was accepted")
	} else {
		wantField(t, err, "start and end")
	}
}

func TestAgendaRefusesEmptyBackwardAndHugeWindows(t *testing.T) {
	store := &fakeStore{}
	svc, err := New(Dependencies{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, query := range []appmodel.CalendarEventListQuery{
		{TeamID: 1, From: from, To: from},
		{TeamID: 1, From: from, To: from.Add(-time.Hour)},
		{TeamID: 1, From: from, To: from.AddDate(2, 0, 0)},
		{TeamID: 1, To: from},
	} {
		_, err := svc.Agenda(context.Background(), query)
		wantField(t, err, "from and to")
	}
	if store.listed != 0 {
		t.Fatalf("a refused window reached the store %d times", store.listed)
	}
}

func TestNewNeedsAStore(t *testing.T) {
	if _, err := New(Dependencies{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New without a store = %v", err)
	}
}
