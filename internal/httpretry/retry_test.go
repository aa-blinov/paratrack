package httpretry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoRebuildsRequestAfterRateLimit(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	var requests atomic.Int32
	response, err := Do(context.Background(), server.Client(), []time.Duration{time.Second}, "test", func() (*http.Request, error) {
		requests.Add(1)
		return http.NewRequest(http.MethodPost, server.URL, strings.NewReader("payload"))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" || attempts.Load() != 2 || requests.Load() != 2 {
		t.Fatalf("body=%q attempts=%d requests=%d", body, attempts.Load(), requests.Load())
	}
}

func TestDoRejectsIncompleteRequestDependencies(t *testing.T) {
	client := &http.Client{}
	factory := func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://example.test", nil)
	}
	var nilFactory RequestFactory
	cases := []struct {
		name    string
		client  *http.Client
		factory RequestFactory
		want    error
	}{
		{name: "nil client", factory: factory, want: ErrNilClient},
		{name: "nil factory", client: client, want: ErrNilRequestFactory},
		{name: "nil request", client: client, factory: func() (*http.Request, error) { return nil, nil }, want: ErrNilRequest},
		{name: "typed nil factory", client: client, factory: nilFactory, want: ErrNilRequestFactory},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response, err := Do(context.Background(), test.client, nil, "test", test.factory)
			if response != nil {
				t.Fatal("Do returned a response for invalid dependencies")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Do error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDoReturnsTypedStatusWithoutUpstreamBody(t *testing.T) {
	const privateResponse = "upstream-internal-detail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, privateResponse)
	}))
	defer server.Close()

	response, err := Do(context.Background(), server.Client(), []time.Duration{time.Second}, "test", func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, server.URL, nil)
	})
	if response != nil {
		response.Body.Close()
		t.Fatal("response should be nil for an HTTP error")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Vendor != "test" || statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("error = %#v, want typed test HTTP 400 error", err)
	}
	if strings.Contains(err.Error(), privateResponse) {
		t.Fatalf("error exposed upstream body: %v", err)
	}
}

func TestWaitParsesRetryAfterHTTPDateAndClampsIt(t *testing.T) {
	var headers http.Header = make(http.Header)
	headers.Set("Retry-After", "Fri, 31 Dec 2099 23:59:59 GMT")
	if got := Wait(headers, time.Second); got != 30*time.Second {
		t.Fatalf("Wait with distant HTTP-date = %s, want 30s clamp", got)
	}

	headers.Set("Retry-After", "not a date")
	if got := Wait(headers, 7*time.Second); got != 7*time.Second {
		t.Fatalf("Wait with invalid Retry-After = %s, want fallback 7s", got)
	}
}
