package appmodel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/integrationport"
)

func TestSensitiveApplicationFieldsAreExcludedFromJSON(t *testing.T) {
	stripeBody := "stripe-body-marker"
	cases := []struct {
		name   string
		value  any
		secret string
	}{
		{"registration password", RegistrationRequest{Password: "registration-password-marker"}, "registration-password-marker"},
		{"account password hash", AccountCreateRequest{PasswordHash: "account-hash-marker"}, "account-hash-marker"},
		{"session token", AuthSessionCreateRequest{Token: "session-create-token-marker"}, "session-create-token-marker"},
		{"session deletion token", AuthSessionDeleteRequest{Token: "session-delete-token-marker"}, "session-delete-token-marker"},
		{"session touch token", AuthSessionTouchRequest{Token: "session-touch-token-marker"}, "session-touch-token-marker"},
		{"api token lookup", APITokenLookupRequest{Raw: "api-token-marker"}, "api-token-marker"},
		{"password change", PasswordChangeRequest{CurrentPassword: "current-password-marker"}, "current-password-marker"},
		{"password login", PasswordLoginRequest{Password: "login-password-marker"}, "login-password-marker"},
		{"password reset", PasswordResetCompletionRequest{Token: "reset-token-marker"}, "reset-token-marker"},
		{"webhook secret", WebhookRegistrationCommand{Secret: "webhook-command-secret-marker"}, "webhook-command-secret-marker"},
		{"webhook endpoint", WebhookRegistrationCommand{URL: "webhook-endpoint-marker"}, "webhook-endpoint-marker"},
		{"stripe signature", StripePaymentEvent{Signature: "stripe-signature-marker"}, "stripe-signature-marker"},
		{"stripe body", StripePaymentEvent{Body: []byte(stripeBody)}, base64.StdEncoding.EncodeToString([]byte(stripeBody))},
		{"time import credential", ProviderImportRunRequest{Provider: importport.ProviderRequest{Secret: "time-import-secret-marker"}}, "time-import-secret-marker"},
		{"time import credential direct", importport.ProviderRequest{Secret: "direct-time-import-secret-marker"}, "direct-time-import-secret-marker"},
		{"task integration credential", integrationport.ProviderInput{Secret: "task-provider-secret-marker", Config: integrationport.ProviderConfig{Target: "private-provider-config-marker"}}, "private-provider-config-marker"},
		{"workspace stripe key", TeamStripeCredentialsRequest{Key: "stripe-key-marker", Secret: "stripe-secret-marker"}, "stripe-key-marker"},
		{"invite token", TeamInviteAcceptRequest{Token: "invite-request-token-marker"}, "invite-request-token-marker"},
		{"integration secret", IntegrationCreateRequest{Secret: "integration-create-secret-marker", Config: IntegrationConfig{Target: "private-integration-target-marker"}}, "private-integration-target-marker"},
		{"push endpoint", PushSubscribeRequest{Endpoint: "push-request-endpoint-marker"}, "push-request-endpoint-marker"},
		{"push key", PushSubscribeRequest{PublicKey: "push-request-public-key-marker"}, "push-request-public-key-marker"},
		{"push unsubscribe endpoint", PushUnsubscribeRequest{Endpoint: "push-delete-endpoint-marker"}, "push-delete-endpoint-marker"},
		{"push auth secret", PushSubscribeRequest{AuthSecret: "push-request-auth-marker"}, "push-request-auth-marker"},
		{"mail payload", InvoiceEmailEnqueueRequest{Payload: []byte("invoice-payload-marker")}, "Payload"},
		{"mail recipient", InvoiceEmailEnqueueRequest{Recipient: "private-recipient-marker"}, "private-recipient-marker"},
		{"webhook event payload", WebhookDeliveryBatchRequest{Payload: []byte("event-payload-marker")}, "Payload"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON exposed sensitive value: %s", encoded)
			}
		})
	}
}
