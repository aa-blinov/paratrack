package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

type sentryState struct {
	dsn         *url.URL
	environment string
	hub         *sentry.Hub
	tunnel      *http.Client
}

func validateSentryConfig(config SentryConfig) error {
	if math.IsNaN(config.TracesSampleRate) || math.IsInf(config.TracesSampleRate, 0) || config.TracesSampleRate < 0 || config.TracesSampleRate > 1 {
		return fmt.Errorf("sentry trace sample rate must be between 0 and 1")
	}
	if strings.TrimSpace(config.DSN) == "" {
		return nil
	}
	dsn, err := url.Parse(config.DSN)
	if err != nil || (dsn.Scheme != "http" && dsn.Scheme != "https") || dsn.Hostname() == "" || dsn.User == nil || dsn.User.Username() == "" || dsn.Path == "" || dsn.RawQuery != "" || dsn.Fragment != "" {
		return fmt.Errorf("sentry DSN must be an absolute http(s) URL with a public key and project path")
	}
	environment := strings.ToLower(strings.TrimSpace(config.Environment))
	if environment == "" {
		environment = "production"
	}
	if environment != "development" && environment != "test" && dsn.Scheme != "https" {
		return fmt.Errorf("sentry DSN must use https outside development and test")
	}
	return nil
}

// initSentry turns process-wide error reporting on from explicit configuration.
// Self-hosted installs leave the DSN empty and nothing leaves the server.
func initSentry(config SentryConfig, logger *log.Logger) sentryState {
	if config.DSN == "" {
		return sentryState{}
	}
	if config.Environment == "" {
		config.Environment = "production"
	}
	dsn, err := url.Parse(config.DSN)
	if err != nil {
		logger.Printf("sentry: invalid DSN: %v (reporting off)", err)
		return sentryState{}
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:                   config.DSN,
		Environment:           config.Environment,
		Release:               "paratrack@" + assetVersion,
		DataCollection:        sentryPrivacyPolicy(),
		EnableTracing:         config.TracesSampleRate > 0,
		TracesSampleRate:      config.TracesSampleRate,
		BeforeSend:            scrubSentryRequest,
		BeforeSendTransaction: scrubSentryRequest,
	})
	if err != nil {
		logger.Printf("sentry: %v (reporting off)", err)
		return sentryState{}
	}
	logger.Printf("sentry: reporting on (%s, traces %.2f)", config.Environment, config.TracesSampleRate)
	return sentryState{
		dsn: dsn, environment: config.Environment,
		hub:    sentry.NewHub(client, sentry.NewScope()),
		tunnel: newSentryTunnelClient(),
	}
}

func sentryPrivacyPolicy() *sentry.DataCollection {
	return &sentry.DataCollection{
		UserInfo: sentry.Set(false),
		Cookies:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		HTTPHeaders: &sentry.HeaderCollectionConfig{
			Request:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		},
		HTTPBodies:  []sentry.BodyType{},
		QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
	}
}

func newSentryTunnelClient() *http.Client {
	var transport *http.Transport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	} else {
		// The configured transport is arbitrary and cannot be cloned safely.
		// Use a separately owned transport with standard defaults instead.
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

// scrubSentryRequest removes query data from telemetry. Password-reset links
// carry a one-time credential in the query string, and other routes may carry
// user-provided data there. Referrer headers can contain the same URL.
func scrubSentryRequest(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil || event.Request == nil {
		return event
	}
	if parsed, err := url.Parse(event.Request.URL); err == nil {
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		event.Request.URL = parsed.String()
	}
	event.Request.QueryString = ""
	event.Request.Data = ""
	event.Request.Cookies = ""
	event.Request.Env = nil
	for key := range event.Request.Headers {
		switch strings.ToLower(key) {
		case "referer", "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
			delete(event.Request.Headers, key)
		}
	}
	return event
}

func (s sentryState) close() {
	if s.tunnel != nil {
		s.tunnel.CloseIdleConnections()
	}
	if s.hub != nil && s.hub.Client() != nil {
		s.hub.Flush(2 * time.Second)
		s.hub.Client().Close()
	}
}

// publicDSN is what base.html hands to the browser SDK ("" = off).
func (s sentryState) publicDSN() string {
	if s.dsn == nil {
		return ""
	}
	return s.dsn.String()
}

// handleSentryTunnel forwards browser envelopes to Sentry. Only envelopes
// for our own DSN pass, so the endpoint can't be used as an open relay.
func (s *Server) handleSentryTunnel(w http.ResponseWriter, r *http.Request) {
	if s.sentry.dsn == nil {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	first, _, _ := bufio.NewReader(bytes.NewReader(body)).ReadLine()
	var hdr struct {
		DSN string `json:"dsn"`
	}
	if json.Unmarshal(first, &hdr) != nil {
		http.Error(w, "bad envelope", 400)
		return
	}
	got, err := url.Parse(hdr.DSN)
	if err != nil || got.Host != s.sentry.dsn.Host || got.Path != s.sentry.dsn.Path {
		http.Error(w, "unknown dsn", 400)
		return
	}
	project := strings.Trim(s.sentry.dsn.Path, "/")
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://"+s.sentry.dsn.Host+"/api/"+project+"/envelope/", bytes.NewReader(body))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	up.Header.Set("Content-Type", "application/x-sentry-envelope")
	resp, err := s.sentry.tunnel.Do(up)
	if err != nil {
		http.Error(w, "sentry unreachable", http.StatusBadGateway)
		return
	}
	resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
}

// withSentry reports panics and every 5xx answer. Internal error details are
// kept in server logs; Sentry receives the request metadata and safe response.
func withSentry(h http.Handler, hub *sentry.Hub) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status >= 500 {
			if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
				hub.CaptureMessage(fmt.Sprintf("%d %s %s: %s", sw.status, r.Method, r.URL.Path, sw.body))
			}
		}
	})
	middleware := sentryhttp.New(sentryhttp.Options{Repanic: true, Timeout: 2 * time.Second}).Handle(inner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := sentry.SetHubOnContext(r.Context(), hub.Clone())
		middleware.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.status >= 500 && len(w.body) < 300 {
		w.body = append(w.body, b[:min(len(b), 300-len(w.body))]...)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap keeps http.ResponseController (flush, deadlines) working.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
