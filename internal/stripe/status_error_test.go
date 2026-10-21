package stripe

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The code of a refused call survives, so the caller can tell an invalid key from an
// outage without reading the text. A transport failure is not a status at all.
func TestCreateCheckoutKeepsTheStatusOfARefusal(t *testing.T) {
	input := CheckoutRequest{InvoiceID: 12, TeamID: 4, TotalCents: 2500, Currency: "USD", InvoiceNumber: "INV-1", SuccessURL: "https://app.example/invoices/12"}
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid API Key provided"}}`)), Header: make(http.Header)}, nil
		})}
		_, err := CreateCheckout(t.Context(), client, "sk_bad", input)
		var status *StatusError
		if !errors.As(err, &status) || status.Code != code {
			t.Fatalf("status %d: error %v, want a *StatusError with that code", code, err)
		}
		if !strings.Contains(status.Body, "Invalid API Key") {
			t.Fatalf("status %d: body %q lost Stripe's text for the log", code, status.Body)
		}
		if !strings.Contains(err.Error(), "stripe returned status") {
			t.Fatalf("status %d: log text %q changed", code, err.Error())
		}
	}
}

func TestCreateCheckoutTransportFailureIsNotAStatus(t *testing.T) {
	input := CheckoutRequest{InvoiceID: 12, TeamID: 4, TotalCents: 2500, Currency: "USD", InvoiceNumber: "INV-1", SuccessURL: "https://app.example/invoices/12"}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: no route to host")
	})}
	_, err := CreateCheckout(t.Context(), client, "sk_test", input)
	var status *StatusError
	if err == nil || errors.As(err, &status) {
		t.Fatalf("a network failure must not look like Stripe's answer: %v", err)
	}
}

func TestExpireCheckoutKeepsTheStatusAndNamesTheCall(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	err := ExpireCheckout(t.Context(), client, "sk_bad", "cs_123")
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusUnauthorized {
		t.Fatalf("expire refused: %v, want a *StatusError with 401", err)
	}
	if !strings.Contains(err.Error(), "while expiring checkout") {
		t.Fatalf("expire log text %q does not name the call", err.Error())
	}
}
