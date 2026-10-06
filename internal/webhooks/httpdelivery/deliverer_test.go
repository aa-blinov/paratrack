package httpdelivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/netpolicy"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func TestDeliverForwardsSignedEventAndClosesResponse(t *testing.T) {
	responseBody := &trackedBody{Reader: strings.NewReader("ok")}
	var got *http.Request
	deliverer, err := New(doerFunc(func(request *http.Request) (*http.Response, error) {
		got = request
		return &http.Response{StatusCode: http.StatusNoContent, Body: responseBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	message := webhookport.DeliveryRequest{
		URL: "https://hooks.example.test/events", Event: "invoice.paid",
		Timestamp: "1730000000", Signature: "signature-v1", SignatureV2: "signature-v2",
		Body: []byte(`{"invoice_id":42}`),
	}

	status, err := deliverer.Deliver(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", status, http.StatusNoContent)
	}
	if got == nil {
		t.Fatal("HTTP client was not called")
	}
	if got.Method != http.MethodPost || got.URL.String() != message.URL {
		t.Fatalf("request = %s %s, want POST %s", got.Method, got.URL, message.URL)
	}
	if got.Header.Get("Content-Type") != "application/json" ||
		got.Header.Get("X-Paratrack-Event") != message.Event ||
		got.Header.Get("X-Paratrack-Timestamp") != message.Timestamp ||
		got.Header.Get("X-Paratrack-Signature") != message.Signature ||
		got.Header.Get("X-Paratrack-Signature-V2") != message.SignatureV2 {
		t.Fatalf("request headers = %#v", got.Header)
	}
	body, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(message.Body) {
		t.Fatalf("request body = %q, want %q", body, message.Body)
	}
	if !responseBody.closed {
		t.Fatal("response body was not closed")
	}
}

func TestDeliverPreservesStatusAndWrapsPrivateTargetError(t *testing.T) {
	responseBody := &trackedBody{Reader: strings.NewReader("")}
	deliverer, err := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: responseBody}, netpolicy.ErrPrivateTarget
	}))
	if err != nil {
		t.Fatal(err)
	}

	status, err := deliverer.Deliver(context.Background(), webhookport.DeliveryRequest{
		URL: "https://hooks.example.test/events",
	})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
	if !errors.Is(err, webhookport.ErrPrivateTarget) || !errors.Is(err, netpolicy.ErrPrivateTarget) {
		t.Fatalf("delivery error = %v, want both private-target sentinels", err)
	}
	if !responseBody.closed {
		t.Fatal("response body was not closed")
	}
}

func TestNewRejectsNilHTTPClient(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New(nil) error = %v, want %v", err, ErrIncompleteDependencies)
	}
}

func TestDeliverWithResponseReturnsTheAnswerBody(t *testing.T) {
	responseBody := &trackedBody{Reader: strings.NewReader(`{"received":true}`)}
	var sent *http.Request
	deliverer, err := New(doerFunc(func(request *http.Request) (*http.Response, error) {
		sent = request
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	message := webhookport.DeliveryRequest{
		URL: "https://hooks.example.test/events", Event: "session.stopped",
		Timestamp: "1730000000", Signature: "signature-v1", SignatureV2: "signature-v2",
		Body: []byte(`{"action":"test"}`),
	}

	status, body, err := deliverer.DeliverWithResponse(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || body != `{"received":true}` {
		t.Fatalf("answer = (%d, %q), want the status and body the receiver replied", status, body)
	}
	if sent == nil || sent.Header.Get("X-Paratrack-Signature") != message.Signature {
		t.Fatalf("the richer call must send the same signed request, headers = %#v", sent)
	}
	if !responseBody.closed {
		t.Fatal("response body was not closed")
	}
}

func TestDeliverWithResponseBoundsWhatAReceiverCanReturn(t *testing.T) {
	responseBody := &trackedBody{Reader: strings.NewReader(strings.Repeat("x", maxResponseBody*2))}
	deliverer, err := New(doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	_, body, err := deliverer.DeliverWithResponse(context.Background(), webhookport.DeliveryRequest{URL: "https://hooks.example.test/events"})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != maxResponseBody {
		t.Fatalf("stored answer = %d bytes, want the %d byte cap", len(body), maxResponseBody)
	}
}
