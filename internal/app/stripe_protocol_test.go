package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/invoicing"
)

// statusTransport answers every request with one status code and a Stripe-shaped body.
type statusTransport int

func (code statusTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: int(code), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid API Key provided"}}`)), Header: make(http.Header)}, nil
}

// Only a key Stripe refuses becomes ErrStripeKeyRejected; an outage keeps no such mark.
func TestStripeAdapterMarksARefusedKeyAndOnlyThat(t *testing.T) {
	request := invoicing.StripeCheckoutRequest{InvoiceID: 12, TeamID: 4, TotalCents: 2500, Currency: "USD", InvoiceNumber: "INV-12", SuccessURL: "https://app.example/invoices/12"}
	for _, tc := range []struct {
		code     int
		rejected bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	} {
		protocol := stripeProtocol{client: &http.Client{Transport: statusTransport(tc.code)}}
		_, _, err := protocol.CreateCheckout(context.Background(), "sk_bad", request)
		if err == nil {
			t.Fatalf("status %d: no error", tc.code)
		}
		if got := errors.Is(err, appmodel.ErrStripeKeyRejected); got != tc.rejected {
			t.Fatalf("status %d: ErrStripeKeyRejected = %v, want %v (error %v)", tc.code, got, tc.rejected, err)
		}
		if !strings.Contains(err.Error(), "Invalid API Key provided") {
			t.Fatalf("status %d: the log loses Stripe's text: %v", tc.code, err)
		}
	}
}
