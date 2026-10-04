// Package providers implements outbound protocols used by the push workflow.
package providers

import (
	"context"
	"errors"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/pushport"
)

var ErrIncompleteDependencies = errors.New("web push adapter dependencies are incomplete")

type Sender struct{ client *http.Client }

func New(client *http.Client) (*Sender, error) {
	if depcheck.IsNil(client) {
		return nil, ErrIncompleteDependencies
	}
	return &Sender{client: client}, nil
}

func (s *Sender) Send(ctx context.Context, sub pushport.Subscription, publicKey, privateKey string, payload []byte) (pushport.SendResult, error) {
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber: "paratrack", VAPIDPublicKey: publicKey,
		VAPIDPrivateKey: privateKey, TTL: 86400, HTTPClient: s.client,
	})
	if err != nil {
		return pushport.SendResult{}, err
	}
	if response == nil {
		return pushport.SendResult{}, errors.New("web push provider returned no response")
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	return pushport.SendResult{
		SubscriptionExpired: response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound,
	}, nil
}
