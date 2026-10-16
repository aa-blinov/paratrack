package teams

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// stripeSettingsStore records what reaches persistence. SetStripeCredentials
// calls no other settings method, so the embedded interface stays nil.
type stripeSettingsStore struct {
	SettingsStore
	saved []appmodel.TeamStripeCredentialsRequest
}

func (s *stripeSettingsStore) SetTeamStripe(_ context.Context, request appmodel.TeamStripeCredentialsRequest) error {
	s.saved = append(s.saved, request)
	return nil
}

func TestSetStripeCredentialsAcceptsOnlyStripeShapes(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		secret  string
		clear   bool
		wantErr bool
	}{
		{name: "live secret key", key: "sk_live_123"},
		{name: "test restricted key with spaces", key: "  rk_test_123  "},
		{name: "webhook secret", secret: "whsec_123"},
		{name: "both values", key: "sk_test_1", secret: "whsec_1"},
		{name: "empty keeps stored values"},
		{name: "clear ignores typed values", key: "not-a-key", clear: true},
		{name: "publishable key", key: "pk_live_123", wantErr: true},
		{name: "plain text", key: "hello", wantErr: true},
		{name: "secret key in webhook field", secret: "sk_live_123", wantErr: true},
		{name: "webhook secret without prefix", secret: "abc123", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stripeSettingsStore{}
			service := &Service{settings: store}
			err := service.SetStripeCredentials(context.Background(), appmodel.TeamStripeCredentialsRequest{
				TeamID: 1, CallerID: 1, Key: tc.key, Secret: tc.secret, Clear: tc.clear,
			})
			if tc.wantErr {
				if !errors.Is(err, appmodel.ErrInvalidTeamSettings) {
					t.Fatalf("err = %v, want ErrInvalidTeamSettings", err)
				}
				if len(store.saved) != 0 {
					t.Fatalf("rejected value reached storage: %+v", store.saved)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(store.saved) != 1 {
				t.Fatalf("saved %d times, want 1", len(store.saved))
			}
		})
	}
}
