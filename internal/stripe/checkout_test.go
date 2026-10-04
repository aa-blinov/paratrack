package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCreateCheckoutBuildsProviderRequest(t *testing.T) {
	var gotForm url.Values
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://api.stripe.com/v1/checkout/sessions" {
			t.Fatalf("request URL = %q", req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer sk_test" {
			t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotForm, err = url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"cs_123","url":"https://checkout.example/cs_123"}`)), Header: make(http.Header)}, nil
	})}
	input := CheckoutRequest{
		InvoiceID: 12, TeamID: 4, TotalCents: 2500, Currency: "USD",
		InvoiceNumber: strings.Repeat("A", 110), SuccessURL: "https://app.example/invoices/12",
	}
	got, err := CreateCheckout(t.Context(), client, "sk_test", input)
	if err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	if got.ID != "cs_123" || got.URL != "https://checkout.example/cs_123" {
		t.Fatalf("session = %+v", got)
	}
	want := map[string]string{
		"mode": "payment", "success_url": "https://app.example/invoices/12?flash=paid",
		"cancel_url": "https://app.example/invoices/12", "metadata[invoice_id]": "12",
		"metadata[team_id]": "4", "line_items[0][price_data][currency]": "usd",
		"line_items[0][price_data][unit_amount]": "2500",
	}
	for key, value := range want {
		if got := gotForm.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if len(gotForm.Get("line_items[0][price_data][product_data][name]")) != 100 {
		t.Errorf("product name length = %d, want 100", len(gotForm.Get("line_items[0][price_data][product_data][name]")))
	}
}

func TestExpireCheckoutUsesSessionEndpointAndSecret(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://api.stripe.com/v1/checkout/sessions/cs_123/expire" {
			t.Fatalf("expiration request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer sk_test" {
			t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"expired"}`)), Header: make(http.Header)}, nil
	})}
	if err := ExpireCheckout(t.Context(), client, "sk_test", "cs_123"); err != nil {
		t.Fatalf("expire checkout: %v", err)
	}
}

func TestVerifyWebhookSignatureRequiresFreshMatchingSecret(t *testing.T) {
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Unix(1790000000, 0)
	sign := func(ts int64, secret string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(mac, "%d.%s", ts, body)
		return fmt.Sprintf("t=%d,v1=%x", ts, mac.Sum(nil))
	}
	if !VerifyWebhookSignature(sign(now.Unix(), "whsec_a"), body, "whsec_a", now) {
		t.Fatal("valid signature rejected")
	}
	for name, header := range map[string]string{
		"wrong secret": sign(now.Unix(), "whsec_b"),
		"stale":        sign(now.Unix()-600, "whsec_a"),
		"missing":      "",
		"malformed":    "t=x,v1=zz",
	} {
		if VerifyWebhookSignature(header, body, "whsec_a", now) {
			t.Errorf("%s signature accepted", name)
		}
	}
	if VerifyWebhookSignature(sign(now.Unix(), "whsec_a"), body, "", now) {
		t.Fatal("signature accepted without configured secret")
	}
}

func TestParseWebhookEvent(t *testing.T) {
	event, err := ParseWebhookEvent([]byte(`{"type":"checkout.session.completed","data":{"object":{"id":"cs_1","metadata":{"invoice_id":"12","team_id":"4"},"payment_status":"paid"}}}`))
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}
	if event.Type != "checkout.session.completed" || event.SessionID != "cs_1" || event.InvoiceID != "12" || event.TeamID != "4" || event.PaymentStatus != "paid" {
		t.Fatalf("parsed event = %+v", event)
	}
	if _, err := ParseWebhookEvent([]byte("{")); err == nil {
		t.Fatal("malformed event JSON accepted")
	}
}
