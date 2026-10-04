package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingWorker struct {
	started chan<- context.Context
	release <-chan struct{}
}

func (w blockingWorker) Shutdown(ctx context.Context) error {
	w.started <- ctx
	<-w.release
	return nil
}

type closeTrackingTransport struct {
	closed atomic.Bool
	calls  atomic.Int32
}

func (*closeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func (t *closeTrackingTransport) CloseIdleConnections() {
	t.calls.Add(1)
	t.closed.Store(true)
}

type countingShutdownWorker struct {
	calls atomic.Int32
	err   error
}

func (w *countingShutdownWorker) Shutdown(context.Context) error {
	w.calls.Add(1)
	return w.err
}

func TestApplicationResourcesCloseShutsWorkersConcurrentlyBeforeClients(t *testing.T) {
	started := make(chan context.Context, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWorkers()
	transport := &closeTrackingTransport{}
	resources := applicationResources{
		webhooks: blockingWorker{started: started, release: release},
		push:     blockingWorker{started: started, release: release},
		clients:  idleHTTPClients{&http.Client{Transport: transport}},
	}

	closed := make(chan error, 1)
	go func() { closed <- resources.Close() }()

	var firstCtx, secondCtx context.Context
	for i := 0; i < 2; i++ {
		select {
		case workerCtx := <-started:
			if i == 0 {
				firstCtx = workerCtx
			} else {
				secondCtx = workerCtx
			}
		case <-time.After(time.Second):
			t.Fatal("workers did not begin shutdown concurrently")
		}
	}
	firstDeadline, firstHasDeadline := firstCtx.Deadline()
	secondDeadline, secondHasDeadline := secondCtx.Deadline()
	if !firstHasDeadline || !secondHasDeadline || !firstDeadline.Equal(secondDeadline) {
		t.Fatalf("worker shutdown deadlines = (%v, %v), want the same bounded deadline", firstDeadline, secondDeadline)
	}
	if transport.closed.Load() {
		t.Fatal("HTTP client closed before worker shutdown completed")
	}

	releaseWorkers()
	if err := <-closed; err != nil {
		t.Fatalf("close application resources: %v", err)
	}
	if !transport.closed.Load() {
		t.Fatal("HTTP client was not closed after worker shutdown")
	}
}

func TestApplicationResourcesCloseIsConcurrentAndIdempotent(t *testing.T) {
	wantErr := errors.New("worker shutdown failed")
	worker := &countingShutdownWorker{err: wantErr}
	transport := &closeTrackingTransport{}
	resources := &applicationResources{
		push:    worker,
		clients: idleHTTPClients{&http.Client{Transport: transport}},
	}

	results := make(chan error, 2)
	var callers sync.WaitGroup
	for range 2 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			results <- resources.Close()
		}()
	}
	callers.Wait()
	close(results)

	for err := range results {
		if !errors.Is(err, wantErr) {
			t.Errorf("Close() error = %v, want wrapped worker error", err)
		}
	}
	if got := worker.calls.Load(); got != 1 {
		t.Errorf("worker shutdown calls = %d, want 1", got)
	}
	if got := transport.calls.Load(); got != 1 {
		t.Errorf("HTTP client close calls = %d, want 1", got)
	}
}
