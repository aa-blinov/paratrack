package pushport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionCredentialsAreExcludedFromJSON(t *testing.T) {
	cases := []struct {
		name   string
		value  Subscription
		secret string
	}{
		{"endpoint", Subscription{Endpoint: "push-endpoint-marker"}, "push-endpoint-marker"},
		{"public key", Subscription{P256DH: "push-key-marker"}, "push-key-marker"},
		{"auth key", Subscription{Auth: "push-auth-marker"}, "push-auth-marker"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON exposed subscription credential: %s", encoded)
			}
		})
	}
}
