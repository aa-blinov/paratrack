package cli

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/model"
)

type countingCloser struct {
	calls int
	err   error
}

func (c *countingCloser) Close() error {
	c.calls++
	return c.err
}

func TestRuntimeCloseIsIdempotent(t *testing.T) {
	wantErr := errors.New("close failed")
	closer := &countingCloser{err: wantErr}
	runtime := &Runtime{serviceCloser: closer}

	if err := runtime.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("first Close() error = %v, want %v", err, wantErr)
	}
	if err := runtime.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("second Close() error = %v, want %v", err, wantErr)
	}
	if closer.calls != 1 {
		t.Fatalf("closer called %d times, want 1", closer.calls)
	}
}

func TestClosedRuntimeDoesNotLoadServices(t *testing.T) {
	loaded := false
	runtime := &Runtime{
		Context: context.Background(),
		ServiceLoader: func(context.Context) (*cliport.Services, io.Closer, error) {
			loaded = true
			return nil, nil, nil
		},
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := runtime.application(); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("application() error = %v, want %v", err, ErrRuntimeClosed)
	}
	if loaded {
		t.Fatal("service loader ran after runtime closed")
	}
}

func TestRuntimeUsesInjectedClock(t *testing.T) {
	want := time.Date(2026, time.October, 2, 12, 30, 0, 0, time.UTC)
	runtime := NewRuntime(nil, nil, nil)
	runtime.Now = func() time.Time { return want }
	if got := runtime.now(); !got.Equal(want) {
		t.Fatalf("runtime clock = %s, want %s", got, want)
	}
}

func TestRuntimeClockRequiresConfiguration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("runtime clock without configuration did not panic")
		}
	}()
	(&Runtime{}).now()
}

type blockingTimerCommands struct {
	cliport.TimerCommands
	started chan struct{}
	release chan struct{}
}

func (s *blockingTimerCommands) ActiveSessions(context.Context, int64) ([]model.ActiveSession, error) {
	close(s.started)
	<-s.release
	return nil, nil
}

type runtimeProjects struct{ cliport.Projects }

type runtimeWorkspace struct{}

func (runtimeWorkspace) DefaultTeam(context.Context) (int64, error) { return 1, nil }
func (runtimeWorkspace) TeamOwnerID(context.Context, int64) (int64, error) {
	return 2, nil
}

type runtimeProjectLookup struct{}

func (runtimeProjectLookup) ProjectIDBySlug(context.Context, string) (int64, error) { return 1, nil }
func (runtimeProjectLookup) GetProjectByID(context.Context, int64) (model.Project, error) {
	return model.Project{ID: 1, TeamID: 1}, nil
}

type runtimeTimerOperations struct{ cliport.TimerOperations }
type runtimeActivityCatalog struct{ cliport.ActivityCatalog }
type runtimeSessionHistory struct{ cliport.SessionHistory }
type runtimeTagging struct{ cliport.Tagging }
type runtimeGoals struct{ cliport.Goals }

type signalCloser struct{ closed chan struct{} }

func (c signalCloser) Close() error {
	close(c.closed)
	return nil
}

func TestRuntimeCloseWaitsForActiveCommand(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	runtime := NewRuntime(nil, io.Discard, io.Discard)
	runtime.ServiceLoader = func(context.Context) (*cliport.Services, io.Closer, error) {
		return &cliport.Services{
			Timers:          &blockingTimerCommands{started: started, release: release},
			TimerOperations: &runtimeTimerOperations{},
			ActivityCatalog: &runtimeActivityCatalog{},
			SessionHistory:  &runtimeSessionHistory{},
			Workspace:       runtimeWorkspace{},
			ProjectLookup:   runtimeProjectLookup{},
			Projects:        runtimeProjects{},
			Tagging:         &runtimeTagging{},
			Goals:           &runtimeGoals{},
		}, signalCloser{closed: closed}, nil
	}

	commandDone := make(chan error, 1)
	go func() { commandDone <- RunWithRuntime([]string{"status"}, runtime) }()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()

	select {
	case <-closed:
		close(release)
		<-commandDone
		t.Fatal("runtime closed services while the command was using them")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	if err := <-commandDone; err != nil {
		t.Fatalf("RunWithRuntime: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("Close did not release command resources")
	}
}
