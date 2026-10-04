package mailport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInvoiceEmailJobSensitiveFieldsAreExcludedFromJSON(t *testing.T) {
	cases := []struct {
		name   string
		value  InvoiceEmailJob
		secret string
	}{
		{"recipient", InvoiceEmailJob{Recipient: "invoice-recipient-marker"}, "invoice-recipient-marker"},
		{"payload", InvoiceEmailJob{Payload: []byte("invoice-mail-marker")}, "Payload"},
		{"lease", InvoiceEmailJob{Lease: "mail-lease-marker"}, "mail-lease-marker"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON exposed invoice email queue value: %s", encoded)
			}
		})
	}
}
