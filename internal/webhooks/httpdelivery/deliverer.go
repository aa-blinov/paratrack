// Package httpdelivery implements the webhook outbound HTTP port.
package httpdelivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/netpolicy"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrIncompleteDependencies = errors.New("webhook HTTP adapter dependencies are incomplete")

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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, message.URL, bytes.NewReader(message.Body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paratrack-Event", message.Event)
	req.Header.Set("X-Paratrack-Timestamp", message.Timestamp)
	req.Header.Set("X-Paratrack-Signature", message.Signature)
	req.Header.Set("X-Paratrack-Signature-V2", message.SignatureV2)
	resp, err := d.client.Do(req)
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if errors.Is(err, netpolicy.ErrPrivateTarget) {
		return status, fmt.Errorf("%w: %w", webhookport.ErrPrivateTarget, err)
	}
	return status, err
}
