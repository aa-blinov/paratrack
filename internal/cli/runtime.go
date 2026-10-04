package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/cliport"
)

var ErrRuntimeClosed = errors.New("CLI runtime is closed")

// Runtime contains the process-facing dependencies used by CLI commands.
// Supplying readers and writers makes command behavior embeddable and
// independently testable without mutating process-global streams.
type Runtime struct {
	Context context.Context
	Now     func() time.Time
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
	// WebRunner is supplied by the process composition root. The CLI owns
	// command parsing and output; the binary owns HTTP server construction.
	WebRunner func(context.Context, string) error
	// ServiceLoader lazily supplies the application graph for commands that
	// need persistence. The process root owns the returned closer.
	ServiceLoader func(context.Context) (*cliport.Services, io.Closer, error)
	services      *cliport.Services
	serviceCloser io.Closer
	serviceErr    error
	servicesReady bool
	commandMu     sync.Mutex
	mu            sync.Mutex
	closed        bool
	closeErr      error
}

func (rt *Runtime) application() (*cliport.Services, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrRuntimeClosed
	}
	if !rt.servicesReady {
		rt.servicesReady = true
		if rt.ServiceLoader == nil {
			rt.serviceErr = cliport.ErrIncompleteServices
		} else {
			rt.services, rt.serviceCloser, rt.serviceErr = rt.ServiceLoader(rt.Context)
			if rt.serviceErr == nil {
				rt.serviceErr = rt.services.Validate()
			}
		}
	}
	return rt.services, rt.serviceErr
}

// Close releases resources opened by the lazy service loader. It waits for an
// active RunWithRuntime command to finish before closing its services. The
// runtime owner calls Close after RunWithRuntime returns.
func (rt *Runtime) Close() error {
	rt.commandMu.Lock()
	defer rt.commandMu.Unlock()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return rt.closeErr
	}
	rt.closed = true
	if rt.serviceCloser != nil {
		rt.closeErr = rt.serviceCloser.Close()
	}
	return rt.closeErr
}

// NewRuntime fills nil streams with the current process defaults.
func NewRuntime(in io.Reader, out, errOut io.Writer) *Runtime {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	return &Runtime{Context: context.Background(), Now: time.Now, In: in, Out: out, Err: errOut}
}

func (rt *Runtime) now() time.Time {
	if rt == nil || rt.Now == nil {
		panic("CLI runtime clock is not configured")
	}
	return rt.Now()
}
