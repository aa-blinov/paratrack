// Package stripe implements the outbound Stripe Checkout and webhook-signature protocols.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type WebhookEvent struct {
	Type          string
	SessionID     string
	InvoiceID     string
	TeamID        string
	PaymentStatus string
}

// ParseWebhookEvent decodes the subset of Stripe's event payload used by the
// invoicing workflow. Numeric metadata remains textual until signature checks
// have succeeded and the HTTP adapter is ready to apply tenant scope.
func ParseWebhookEvent(body []byte) (WebhookEvent, error) {
	var payload struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID       string `json:"id"`
				Metadata struct {
					InvoiceID string `json:"invoice_id"`
					TeamID    string `json:"team_id"`
				} `json:"metadata"`
				PaymentStatus string `json:"payment_status"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return WebhookEvent{}, fmt.Errorf("decode Stripe webhook event: %w", err)
	}
	return WebhookEvent{
		Type: payload.Type, SessionID: payload.Data.Object.ID,
		InvoiceID:     payload.Data.Object.Metadata.InvoiceID,
		TeamID:        payload.Data.Object.Metadata.TeamID,
		PaymentStatus: payload.Data.Object.PaymentStatus,
	}, nil
}

type CheckoutRequest struct {
	InvoiceID     int64
	TeamID        int64
	TotalCents    int
	Currency      string
	InvoiceNumber string
	SuccessURL    string
}

type CheckoutSession struct {
	ID  string
	URL string
}

// CreateCheckout creates one hosted payment session through Stripe's form API.
func CreateCheckout(ctx context.Context, client *http.Client, secret string, input CheckoutRequest) (CheckoutSession, error) {
	if client == nil {
		return CheckoutSession{}, fmt.Errorf("stripe HTTP client is not configured")
	}
	if secret == "" || input.InvoiceID <= 0 || input.TeamID <= 0 || input.TotalCents <= 0 {
		return CheckoutSession{}, fmt.Errorf("invalid Stripe checkout input")
	}
	form := url.Values{
		"mode":                                   {"payment"},
		"success_url":                            {input.SuccessURL + "?flash=paid"},
		"cancel_url":                             {input.SuccessURL},
		"client_reference_id":                    {strconv.FormatInt(input.InvoiceID, 10)},
		"metadata[invoice_id]":                   {strconv.FormatInt(input.InvoiceID, 10)},
		"metadata[team_id]":                      {strconv.FormatInt(input.TeamID, 10)},
		"line_items[0][quantity]":                {"1"},
		"line_items[0][price_data][currency]":    {strings.ToLower(input.Currency)},
		"line_items[0][price_data][unit_amount]": {strconv.Itoa(input.TotalCents)},
	}
	label := "Invoice " + input.InvoiceNumber
	if len(label) > 100 {
		label = label[:100]
	}
	form.Set("line_items[0][price_data][product_data][name]", label)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("build Stripe checkout request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("create Stripe checkout session: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("read Stripe checkout response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return CheckoutSession{}, fmt.Errorf("stripe returned status %d: %s", resp.StatusCode, string(body)[:min(180, len(body))])
	}
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return CheckoutSession{}, fmt.Errorf("decode Stripe checkout response: %w", err)
	}
	if result.ID == "" || result.URL == "" {
		return CheckoutSession{}, fmt.Errorf("stripe returned an incomplete checkout session")
	}
	return CheckoutSession{ID: result.ID, URL: result.URL}, nil
}

// ExpireCheckout closes an open hosted session that the application could not
// persist. Stripe refuses to expire sessions that have already completed.
func ExpireCheckout(ctx context.Context, client *http.Client, secret, sessionID string) error {
	if client == nil || secret == "" || strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("invalid Stripe checkout expiration input")
	}
	endpoint := "https://api.stripe.com/v1/checkout/sessions/" + url.PathEscape(sessionID) + "/expire"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build Stripe checkout expiration request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("expire Stripe checkout session: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("read Stripe checkout expiration response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stripe returned status %d while expiring checkout: %s", resp.StatusCode, string(body)[:min(180, len(body))])
	}
	return nil
}

// VerifyWebhookSignature checks Stripe's timestamped HMAC-SHA256 signature.
// Signatures outside the five-minute tolerance or without a configured secret fail.
func VerifyWebhookSignature(header string, body []byte, secret string, now time.Time) bool {
	if secret == "" || header == "" {
		return false
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(unix, 0)).Abs() > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, signature := range signatures {
		got, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}
