package webhooks

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/aa-blinov/paratrack/internal/testutil"
	webhookhttp "github.com/aa-blinov/paratrack/internal/webhooks/httpdelivery"
)

func TestWebhookRetriesAndSignsTimestamp(t *testing.T) {
	calls := 0
	var signatureValid bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get("X-Paratrack-Timestamp")
		signatureValid = r.Header.Get("X-Paratrack-Signature-V2") == SignPayload("s3cret", append([]byte(timestamp+"."), body...))
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := database.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`); err != nil {
		t.Fatal(err)
	}
	hookSummary, err := database.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: 1, CallerID: 1, URL: server.URL, Secret: "s3cret", Events: "*",
	})
	if err != nil {
		t.Fatal(err)
	}
	hook, err := database.GetWebhook(ctx, 1, hookSummary.ID)
	if err != nil {
		t.Fatal(err)
	}
	auditService, err := audit.New(database)
	if err != nil {
		t.Fatal(err)
	}
	deliverer, err := webhookhttp.New(netclients.Webhook(true))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(database, deliverer, time.Now, auditService, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	service.deliverWithBackoff(ctx, hook, "session.stopped", []byte(`{"x":1}`), []time.Duration{0, 0, 0})
	if calls != 3 || !signatureValid {
		t.Fatalf("calls %d signatureValid %v, want 3 attempts and a valid V2 signature", calls, signatureValid)
	}
	deliveries, err := database.ListWebhookDeliveries(ctx, 1, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 3 || deliveries[0].Status != http.StatusOK {
		t.Errorf("delivery log %+v", deliveries)
	}
}
