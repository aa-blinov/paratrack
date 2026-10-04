package tracking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type sessionEditStoreStub struct {
	SessionStore
	SessionQueryStore
	current model.Session
	updated appmodel.SessionUpdateRequest
	err     error
}

func (stub *sessionEditStoreStub) GetSession(context.Context, int64, int64) (model.Session, error) {
	return stub.current, stub.err
}

func (stub *sessionEditStoreStub) UpdateSessionFields(_ context.Context, request appmodel.SessionUpdateRequest) error {
	stub.updated = request
	return stub.err
}

func TestUpdateFieldsAppliesDurationToSelectedStart(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	selectedStart := start.Add(time.Hour)
	seconds := 90 * 60
	store := &sessionEditStoreStub{current: model.Session{StartAt: start}}
	service := &Service{queries: store, sessions: store}
	err := service.UpdateFields(context.Background(), appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, DurationSeconds: &seconds,
		Update: appmodel.SessionUpdate{StartAt: &selectedStart, UpdatedAt: selectedStart},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := store.updated.Update
	if got.EndAt == nil || !got.EndAt.Equal(selectedStart.Add(time.Duration(seconds)*time.Second)) || got.AccumulatedSeconds == nil || *got.AccumulatedSeconds != seconds {
		t.Fatalf("applied duration update = %+v", got)
	}
	if store.updated.DurationSeconds != nil || store.updated.RecomputeDuration {
		t.Fatalf("unresolved edit intent reached persistence: %+v", store.updated)
	}
}

func TestUpdateFieldsRecomputesDurationFromCurrentStart(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	store := &sessionEditStoreStub{current: model.Session{StartAt: start}}
	service := &Service{queries: store, sessions: store}
	err := service.UpdateFields(context.Background(), appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, RecomputeDuration: true,
		Update: appmodel.SessionUpdate{EndAt: &end, UpdatedAt: end},
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.updated.Update.AccumulatedSeconds == nil || *store.updated.Update.AccumulatedSeconds != 2*60*60 {
		t.Fatalf("recomputed duration = %+v", store.updated.Update.AccumulatedSeconds)
	}
	if store.updated.RecomputeDuration {
		t.Fatal("recompute intent reached persistence")
	}
}

func TestUpdateFieldsRejectsInvalidSessionBeforePersistence(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(-time.Minute)
	store := &sessionEditStoreStub{current: model.Session{StartAt: start}}
	service := &Service{queries: store, sessions: store}
	err := service.UpdateFields(context.Background(), appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, RecomputeDuration: true,
		Update: appmodel.SessionUpdate{EndAt: &end, UpdatedAt: start},
	})
	if !errors.Is(err, appmodel.ErrInvalidSessionPeriod) {
		t.Fatalf("invalid interval error = %v", err)
	}
	if store.updated.SessionID != 0 {
		t.Fatalf("invalid interval reached persistence: %+v", store.updated)
	}
}
