// Package httpretry owns bounded retries for rate-limited HTTP APIs.
package httpretry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

const maxRetryWait = 30 * time.Second

var (
	ErrNilContext        = errors.New("HTTP retry context is nil")
	ErrNilClient         = errors.New("HTTP retry client is nil")
	ErrNilRequestFactory = errors.New("HTTP retry request factory is nil")
	ErrNilRequest        = errors.New("HTTP retry request factory returned a nil request")
)

// RequestFactory rebuilds an equivalent request for each attempt. Requests
// with bodies must provide a fresh body reader on every call.
type RequestFactory func() (*http.Request, error)

// StatusError describes an unsuccessful upstream HTTP response without
// retaining or exposing its response body. Provider bodies are untrusted and
// may contain account data or implementation details.
type StatusError = providerstatus.Error

// Do retries rate-limited requests according to waits and returns a typed
// status error without exposing a remote response body.
func Do(ctx context.Context, client *http.Client, waits []time.Duration, vendor string, makeRequest RequestFactory) (*http.Response, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if client == nil {
		return nil, ErrNilClient
	}
	if makeRequest == nil {
		return nil, ErrNilRequestFactory
	}
	for attempt := 0; ; attempt++ {
		req, err := makeRequest()
		if err != nil {
			return nil, err
		}
		if req == nil {
			return nil, ErrNilRequest
		}
		resp, err := client.Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return resp, nil
		}
		limited := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable ||
			(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-Ratelimit-Remaining") == "0"))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		if !limited || attempt >= len(waits) {
			return nil, &StatusError{Vendor: vendor, StatusCode: resp.StatusCode}
		}
		timer := time.NewTimer(Wait(resp.Header, waits[attempt]))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Wait parses common rate-limit headers and clamps waits to a bounded window.
func Wait(headers http.Header, fallback time.Duration) time.Duration {
	wait := fallback
	retryAfter := headers.Get("Retry-After")
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		// Clamp before converting: a hostile or malformed large integer must
		// not overflow time.Duration and turn the requested wait into zero.
		if seconds >= int(maxRetryWait/time.Second) {
			wait = maxRetryWait
		} else {
			wait = time.Duration(seconds) * time.Second
		}
	} else if at, err := http.ParseTime(retryAfter); err == nil {
		wait = time.Until(at)
	} else if reset, err := strconv.ParseInt(headers.Get("X-Ratelimit-Reset"), 10, 64); err == nil && reset > 1e9 {
		if reset > 1e12 { // Some APIs send milliseconds.
			reset /= 1000
		}
		wait = time.Until(time.Unix(reset, 0))
	}
	if wait < 0 {
		wait = 0
	}
	if wait > maxRetryWait {
		wait = maxRetryWait
	}
	return wait
}
