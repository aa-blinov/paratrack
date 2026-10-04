// Package web serves paratrack's embedded HTTP UI.
//
// All static assets and templates live under the project-root web/
// directory and are compiled into the binary via go:embed — the result
// is a single executable that opens a complete time-tracker at the
// configured address.
package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
)

// Server is the HTTP front-end for paratrack.
type Server struct {
	config      Config
	sentry      sentryState
	services    Dependencies
	runtime     RuntimeDependencies
	logger      *log.Logger
	addr        string
	tmpl        *template.Template
	httpd       *http.Server
	lifecycleMu sync.Mutex
	started     bool
	requestMu   sync.Mutex
	draining    bool
	requests    sync.WaitGroup
}

// ErrNilServerContext indicates that the server lifecycle was started without
// a cancellation context.
var ErrNilServerContext = errors.New("HTTP server context is nil")

// ErrServerAlreadyStarted indicates that the one-shot server lifecycle has
// already been entered, including when a prior start attempt failed.
var ErrServerAlreadyStarted = errors.New("HTTP server lifecycle has already started")

var ErrMissingLogger = errors.New("HTTP logger is required")
var ErrMissingClock = errors.New("HTTP clock is required")

// New constructs a Server with process configuration supplied by
// the composition root. It does not bind the socket.
func New(services Dependencies, addr string, config Config, runtime RuntimeDependencies) (*Server, error) {
	if config.Logger == nil {
		return nil, ErrMissingLogger
	}
	if config.Now == nil {
		return nil, ErrMissingClock
	}
	if err := validateSentryConfig(config.Sentry); err != nil {
		return nil, fmt.Errorf("invalid HTTP Sentry configuration: %w", err)
	}
	serviceGraph := services
	config.TrustedProxies = append(config.TrustedProxies[:0:0], config.TrustedProxies...)
	if err := serviceGraph.Validate(); err != nil {
		return nil, err
	}
	if config.OIDCEnabled && depcheck.IsNil(runtime.OIDC) {
		return nil, fmt.Errorf("incomplete HTTP runtime dependencies: OIDC provider")
	}
	if depcheck.IsNil(runtime.WebhookDeliveryWorker) {
		return nil, fmt.Errorf("incomplete HTTP runtime dependencies: webhook delivery worker")
	}
	if mailport.Available(runtime.Mailer) && depcheck.IsNil(runtime.MailQueueWorker) {
		return nil, fmt.Errorf("incomplete HTTP runtime dependencies: invoice mail worker")
	}
	if assetVersionErr != nil {
		return nil, fmt.Errorf("hash embedded static assets: %w", assetVersionErr)
	}
	sentry := initSentry(config.Sentry, config.Logger)
	if err := i18n.Validate(); err != nil {
		sentry.close()
		return nil, fmt.Errorf("load translations: %w", err)
	}
	if _, err := appImportMap(); err != nil {
		sentry.close()
		return nil, fmt.Errorf("build frontend import map: %w", err)
	}
	tmpl, err := parseTemplates(sentry)
	if err != nil {
		sentry.close()
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	s := &Server{
		config:   config,
		sentry:   sentry,
		services: serviceGraph,
		runtime:  runtime,
		logger:   config.Logger,
		addr:     addr,
		tmpl:     tmpl,
	}
	handler := s.routes()
	if sentry.hub != nil {
		handler = withSentry(handler, sentry.hub)
	}
	handler = s.trackRequests(handler)
	s.httpd = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

// ListenAndServeContext starts the server lifecycle once and serves until the
// listener fails or the parent context is cancelled. Shutdown first drains
// requests, then closes remaining connections on timeout before releasing
// workers and storage.
func (s *Server) ListenAndServeContext(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if s.started {
		s.lifecycleMu.Unlock()
		return ErrServerAlreadyStarted
	}
	s.started = true
	s.lifecycleMu.Unlock()

	if ctx == nil {
		s.sentry.close()
		return ErrNilServerContext
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.sentry.close()
		return err
	}
	s.logger.Printf("paratrack web: listening on http://%s", ln.Addr())
	stopWorker := s.startMailWorker(ctx)
	stopWebhookWorker := s.startWebhookWorker(ctx)
	serveResult := make(chan error, 1)
	go func() { serveResult <- s.httpd.Serve(ln) }()
	var serveErr error
	serveFinished := false
	select {
	case serveErr = <-serveResult:
		serveFinished = true
	case <-ctx.Done():
	}
	s.beginDrain()
	shutdownCtx, cancelHTTP := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s.httpd.Shutdown(shutdownCtx); err != nil {
		s.logger.Printf("paratrack web: graceful shutdown: %v", err)
		if closeErr := s.httpd.Close(); closeErr != nil {
			s.logger.Printf("paratrack web: force close: %v", closeErr)
		}
	}
	cancelHTTP()
	if !serveFinished {
		serveErr = <-serveResult
	}
	s.requests.Wait() // never close dependencies while a handler may use them
	stopWorker()
	stopWebhookWorker()
	s.sentry.close()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}

func (s *Server) startWebhookWorker(parent context.Context) func() {
	if depcheck.IsNil(s.runtime.WebhookDeliveryWorker) {
		return func() {}
	}
	workerCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runtime.WebhookDeliveryWorker.Run(workerCtx, time.Second)
	}()
	return func() {
		cancel()
		<-done // preserve the database until all claimed deliveries unwind
	}
}

func (s *Server) trackRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requestMu.Lock()
		if s.draining {
			s.requestMu.Unlock()
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		s.requests.Add(1)
		s.requestMu.Unlock()
		defer s.requests.Done()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) beginDrain() {
	s.requestMu.Lock()
	s.draining = true
	s.requestMu.Unlock()
}

func (s *Server) startMailWorker(parent context.Context) func() {
	workerCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	if mailport.Available(s.runtime.Mailer) {
		go func() {
			defer close(done)
			s.runtime.MailQueueWorker.Run(workerCtx, func(deliveryCtx context.Context, message mailport.Message) error {
				return s.deliver(deliveryCtx, message)
			}, time.Second)
		}()
	} else {
		close(done) // keep pending outbox rows untouched until SMTP is configured
	}
	return func() {
		cancel()
		<-done // drain current bounded SMTP call before the DB is closed
	}
}
