package tracking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type sessionEditStoreStub struct {
	SessionStore
	updated appmodel.SessionUpdateRequest
	err     error
}

func (stub *sessionEditStoreStub) UpdateSessionFields(_ context.Context, request appmodel.SessionUpdateRequest) error {
	stub.updated = request
	return stub.err
}

func TestUpdateFieldsPassesEditIntentToTransactionalStore(t *testing.T) {
	store := &sessionEditStoreStub{}
	service := &Service{sessions: store}
	seconds := 90 * 60
	updatedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	request := appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, DurationSeconds: &seconds,
		Update: appmodel.SessionUpdate{UpdatedAt: updatedAt},
	}
	if err := service.UpdateFields(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if store.updated.DurationSeconds == nil || *store.updated.DurationSeconds != seconds || store.updated.Update.AccumulatedSeconds != nil {
		t.Fatalf("store request did not preserve duration intent: %+v", store.updated)
	}
}

func TestUpdateFieldsRejectsInvalidDurationBeforePersistence(t *testing.T) {
	store := &sessionEditStoreStub{}
	service := &Service{sessions: store}
	seconds := -1
	updatedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	err := service.UpdateFields(context.Background(), appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, DurationSeconds: &seconds,
		Update: appmodel.SessionUpdate{UpdatedAt: updatedAt},
	})
	if !errors.Is(err, appmodel.ErrInvalidSessionLength) {
		t.Fatalf("invalid duration error = %v", err)
	}
	if store.updated.SessionID != 0 {
		t.Fatalf("invalid duration reached persistence: %+v", store.updated)
	}
}

func TestUpdateFieldsPropagatesTransactionalIntervalError(t *testing.T) {
	wantErr := appmodel.ErrInvalidSessionPeriod
	store := &sessionEditStoreStub{err: wantErr}
	service := &Service{sessions: store}
	updatedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	err := service.UpdateFields(context.Background(), appmodel.SessionUpdateRequest{
		TeamID: 4, CallerID: 12, SessionID: 31, RecomputeDuration: true,
		Update: appmodel.SessionUpdate{UpdatedAt: updatedAt},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("transactional interval error = %v, want %v", err, wantErr)
	}
}
