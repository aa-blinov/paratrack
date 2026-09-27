package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

// sentryDSN is the parsed DSN when reporting is on; the browser SDK gets
// the same one and sends through /sentry-tunnel (same path as Go events).
var sentryDSN *url.URL

// initSentry turns error reporting on when PARATRACK_SENTRY_DSN is set.
// Self-hosted installs leave it empty and nothing leaves the server.
func initSentry() bool {
	dsn := os.Getenv("PARATRACK_SENTRY_DSN")
	if dsn == "" {
		return false
	}
	rate := 1.0
	if v, err := strconv.ParseFloat(os.Getenv("PARATRACK_SENTRY_TRACES"), 64); err == nil {
		rate = v
	}
	env := os.Getenv("PARATRACK_ENV")
	if env == "" {
		env = "production"
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		Release:          "paratrack@" + assetVersion,
		EnableTracing:    rate > 0,
		TracesSampleRate: rate,
	}); err != nil {
		log.Printf("sentry: %v (reporting off)", err)
		return false
	}
	sentryDSN, _ = url.Parse(dsn)
	sentryEnv = env
	log.Printf("sentry: reporting on (%s, traces %.2f)", env, rate)
	return true
}

var sentryEnv string

// sentryPublicDSN is what base.html hands to the browser SDK ("" = off).
func sentryPublicDSN() string {
	if sentryDSN == nil {
		return ""
	}
	return sentryDSN.String()
}

// handleSentryTunnel forwards browser envelopes to Sentry. Only envelopes
// for our own DSN pass, so the endpoint can't be used as an open relay.
func (s *Server) handleSentryTunnel(w http.ResponseWriter, r *http.Request) {
	if sentryDSN == nil {
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
	if err != nil || got.Host != sentryDSN.Host || got.Path != sentryDSN.Path {
		http.Error(w, "unknown dsn", 400)
		return
	}
	project := strings.Trim(sentryDSN.Path, "/")
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://"+sentryDSN.Host+"/api/"+project+"/envelope/", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	up.Header.Set("Content-Type", "application/x-sentry-envelope")
	resp, err := tunnelClient.Do(up)
	if err != nil {
		http.Error(w, "sentry unreachable", http.StatusBadGateway)
		return
	}
	resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
}

var tunnelClient = &http.Client{Timeout: 5 * time.Second}

// withSentry reports panics and every 5xx answer. Handlers answer
// failures with http.Error(w, err.Error(), 500), so the body carries the
// cause; the first bytes of it go into the event.
func withSentry(h http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status >= 500 {
			if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
				hub.CaptureMessage(fmt.Sprintf("%d %s %s: %s", sw.status, r.Method, r.URL.Path, sw.body))
			}
		}
	})
	return sentryhttp.New(sentryhttp.Options{Repanic: true, Timeout: 2 * time.Second}).Handle(inner)
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
