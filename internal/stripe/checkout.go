// Package stripe implements the outbound Stripe Checkout and webhook-signature protocols.
// The wire protocol runs through the official stripe-go SDK: it pins the API
// version it was built against, retries transient failures with its own
// idempotency keys, and tracks Stripe's protocol changes as they are
// announced. The webhook signature check is the one documented, stable
// scheme (t=..., v1=..., HMAC-SHA256) and stays in this package so the
// verification keeps anchoring at the moment the request arrived, not at
// the SDK's own wall clock.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v87"
)

// StatusError is a Stripe answer that was not 200. The code tells an invalid key
// (401, 403) from an outage (5xx) without reading the text; the text stays for the log.
type StatusError struct {
	Code int
	Body string
	// Action names the call when it is not the checkout itself, so the log says which.
	Action string
}

func (e *StatusError) Error() string {
	if e.Action == "" {
		return fmt.Sprintf("stripe returned status %d: %s", e.Code, e.Body)
	}
	return fmt.Sprintf("stripe returned status %d while %s: %s", e.Code, e.Action, e.Body)
}

// translateError maps the SDK's typed answer onto the status error the
// application already knows how to read (invalid key from outage).
func translateError(err error, action string) error {
	var api *stripe.Error
	if errors.As(err, &api) {
		body := err.Error()
		return &StatusError{Code: api.HTTPStatusCode, Body: body[:min(180, len(body))], Action: action}
	}
	return err
}

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

// CreateCheckout creates one hosted payment session. The key is per
// workspace, so the client is built per call; the HTTP client is the one the
// composition root wired, with its timeouts and connection settings.
func CreateCheckout(ctx context.Context, client *http.Client, secret string, input CheckoutRequest) (CheckoutSession, error) {
	if client == nil {
		return CheckoutSession{}, fmt.Errorf("stripe HTTP client is not configured")
	}
	if secret == "" || input.InvoiceID <= 0 || input.TeamID <= 0 || input.TotalCents <= 0 {
		return CheckoutSession{}, fmt.Errorf("invalid Stripe checkout input")
	}
	label := "Invoice " + input.InvoiceNumber
	if len(label) > 100 {
		label = label[:100]
	}
	sc := stripe.NewClient(secret, stripe.WithBackends(stripe.NewBackends(client)))
	session, err := sc.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String("payment"),
		SuccessURL:        stripe.String(input.SuccessURL + "?flash=paid"),
		CancelURL:         stripe.String(input.SuccessURL),
		ClientReferenceID: stripe.String(strconv.FormatInt(input.InvoiceID, 10)),
		Metadata: map[string]string{
			"invoice_id": strconv.FormatInt(input.InvoiceID, 10),
			"team_id":    strconv.FormatInt(input.TeamID, 10),
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(strings.ToLower(input.Currency)),
				UnitAmount: stripe.Int64(int64(input.TotalCents)),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(label),
				},
			},
		}},
	})
	if err != nil {
		return CheckoutSession{}, translateError(err, "")
	}
	if session == nil || session.ID == "" || session.URL == "" {
		return CheckoutSession{}, fmt.Errorf("stripe returned an incomplete checkout session")
	}
	return CheckoutSession{ID: session.ID, URL: session.URL}, nil
}

// ExpireCheckout closes an open hosted session that the application could not
// persist. Stripe refuses to expire sessions that have already completed.
func ExpireCheckout(ctx context.Context, client *http.Client, secret, sessionID string) error {
	if client == nil || secret == "" || strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("invalid Stripe checkout expiration input")
	}
	sc := stripe.NewClient(secret, stripe.WithBackends(stripe.NewBackends(client)))
	_, err := sc.V1CheckoutSessions.Expire(ctx, sessionID, &stripe.CheckoutSessionExpireParams{})
	if err != nil {
		return translateError(err, "expiring checkout")
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
