// Package httpdelivery implements the webhook outbound HTTP port.
package httpdelivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/netpolicy"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrIncompleteDependencies = errors.New("webhook HTTP adapter dependencies are incomplete")

// maxResponseBody bounds what one receiver can push into the delivery history.
const maxResponseBody = 8 << 10

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Deliverer struct{ client HTTPDoer }

func New(client HTTPDoer) (*Deliverer, error) {
	if depcheck.IsNil(client) {
		return nil, ErrIncompleteDependencies
	}
	return &Deliverer{client: client}, nil
}

func (d *Deliverer) Deliver(ctx context.Context, message webhookport.DeliveryRequest) (int, error) {
	status, _, err := d.DeliverWithResponse(ctx, message)
	return status, err
}

// DeliverWithResponse sends the same signed request and also returns the
// receiver's answer body. The delivery history shows it so an integrator can
// see what their endpoint replied instead of guessing from a status code. The
// read is bounded: a receiver cannot stream an unbounded body into the
// process.
func (d *Deliverer) DeliverWithResponse(ctx context.Context, message webhookport.DeliveryRequest) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, message.URL, bytes.NewReader(message.Body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paratrack-Event", message.Event)
	req.Header.Set("X-Paratrack-Timestamp", message.Timestamp)
	req.Header.Set("X-Paratrack-Signature", message.Signature)
	req.Header.Set("X-Paratrack-Signature-V2", message.SignatureV2)
	resp, err := d.client.Do(req)
	status := 0
	var responseBody string
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			// One byte past the limit tells a truncated body apart from one that
			// happens to end exactly at it.
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
			_ = resp.Body.Close()
			if len(body) > maxResponseBody {
				body = body[:maxResponseBody]
			}
			if readErr == nil {
				responseBody = string(body)
			}
		}
	}
	if errors.Is(err, netpolicy.ErrPrivateTarget) {
		return status, responseBody, fmt.Errorf("%w: %w", webhookport.ErrPrivateTarget, err)
	}
	return status, responseBody, err
}
