package webhookport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSensitiveQueueFieldsAreExcludedFromJSON(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		secret string
	}{
		{"webhook secret", Webhook{Secret: "webhook-secret-marker"}, "webhook-secret-marker"},
		{"delivery URL", DeliveryJob{URL: "delivery-url-marker"}, "delivery-url-marker"},
		{"delivery secret", DeliveryJob{Secret: "delivery-secret-marker"}, "delivery-secret-marker"},
		{"delivery payload", DeliveryJob{Payload: []byte("delivery-payload-marker")}, "Payload"},
		{"delivery lease", DeliveryJob{Lease: "delivery-lease-marker"}, "delivery-lease-marker"},
		{"event payload", CommittedEvent{Payload: []byte("outbox-payload-marker")}, "Payload"},
		{"event lease", CommittedEvent{Lease: "event-lease-marker"}, "event-lease-marker"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON exposed sensitive queue value: %s", encoded)
			}
		})
	}
}
