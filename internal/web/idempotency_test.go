package web

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// fakeIdempotency keeps keys in memory, with the same claim rules as the store.
type fakeIdempotency struct {
	keys map[string]int
	busy map[string]bool
}

func (f *fakeIdempotency) ClaimIdempotencyKey(_ context.Context, request appmodel.IdempotencyRequest) (bool, int, error) {
	key := request.Key
	if status, ok := f.keys[key]; ok {
		return true, status, nil
	}
	if f.busy[key] {
		return false, 0, model.ErrRequestInProgress
	}
	f.busy[key] = true
	return false, 0, nil
}

func (f *fakeIdempotency) FinishIdempotencyKey(_ context.Context, request appmodel.IdempotencyRequest) error {
	f.keys[request.Key] = request.Status
	delete(f.busy, request.Key)
	return nil
}

func (f *fakeIdempotency) ReleaseIdempotencyKey(_ context.Context, request appmodel.IdempotencyRequest) error {
	delete(f.busy, request.Key)
	return nil
}

func idempotencyTestServer(store IdempotencyStore) *Server {
	return &Server{
		config:   Config{Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }},
		services: Dependencies{Idempotency: store},
		logger:   log.New(io.Discard, "", 0),
	}
}

func idempotencyRequest(key string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	r.Header.Set("Idempotency-Key", key)
	return r.WithContext(WithUser(r.Context(), appmodel.UserIdentity{ID: 7}))
}

// A replayed write with a finished key is answered with its first status and
// does not run again.
func TestIdempotentWriteRunsOnceForAKey(t *testing.T) {
	store := &fakeIdempotency{keys: map[string]int{}, busy: map[string]bool{}}
	s := idempotencyTestServer(store)
	runs := 0
	handler := s.idempotent(func(w http.ResponseWriter, _ *http.Request) {
		runs++
		w.WriteHeader(http.StatusNoContent)
	})

	for range 2 {
		rec := httptest.NewRecorder()
		handler(rec, idempotencyRequest("queued-1"))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	}
	if runs != 1 {
		t.Fatalf("handler ran %d times for one key, want 1", runs)
	}
}

// A refused write releases its key, so a corrected retry with the same key runs.
func TestRefusedWriteReleasesItsKey(t *testing.T) {
	store := &fakeIdempotency{keys: map[string]int{}, busy: map[string]bool{}}
	s := idempotencyTestServer(store)
	runs := 0
	handler := s.idempotent(func(w http.ResponseWriter, _ *http.Request) {
		runs++
		if runs == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	handler(rec, idempotencyRequest("queued-2"))
	rec = httptest.NewRecorder()
	handler(rec, idempotencyRequest("queued-2"))
	if rec.Code != http.StatusNoContent || runs != 2 {
		t.Fatalf("retry status = %d after %d runs, want 204 after 2", rec.Code, runs)
	}
}

// A key held by an unfinished attempt answers 409 and does not run the write.
func TestKeyInProgressIsRefusedWithoutRunning(t *testing.T) {
	store := &fakeIdempotency{keys: map[string]int{}, busy: map[string]bool{"queued-3": true}}
	s := idempotencyTestServer(store)
	handler := s.idempotent(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run while the key is in progress")
	})

	rec := httptest.NewRecorder()
	handler(rec, idempotencyRequest("queued-3"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

// Without the header the write runs as it always did.
func TestWriteWithoutKeyIsUnchanged(t *testing.T) {
	s := idempotencyTestServer(&fakeIdempotency{keys: map[string]int{}, busy: map[string]bool{}})
	runs := 0
	handler := s.idempotent(func(w http.ResponseWriter, _ *http.Request) {
		runs++
		w.WriteHeader(http.StatusNoContent)
	})
	r := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	handler(httptest.NewRecorder(), r.WithContext(WithUser(r.Context(), appmodel.UserIdentity{ID: 7})))
	handler(httptest.NewRecorder(), r.WithContext(WithUser(r.Context(), appmodel.UserIdentity{ID: 7})))
	if runs != 2 {
		t.Fatalf("handler ran %d times without a key, want 2", runs)
	}
}
