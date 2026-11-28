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

func TestQueuedDeliverySignsTimestampAndRetriesTransientFailure(t *testing.T) {
	calls := 0
	var signatureValid bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get("X-Paratrack-Timestamp")
		signatureValid = r.Header.Get("X-Paratrack-Signature-V2") == SignPayload("s3cret", append([]byte(timestamp+"."), body...))
		if calls == 1 {
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
	if _, err := database.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (1,1,'owner',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	hookSummary, err := database.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: 1, CallerID: 1, URL: server.URL, Secret: "s3cret", Events: "*",
	})
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
	if err := database.EnqueueWebhookDeliveries(ctx, appmodel.WebhookDeliveryBatchRequest{
		TeamID: 1, WebhookIDs: []int64{hookSummary.ID}, Event: "session.stopped", Payload: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	job, ok, err := database.ClaimWebhookDelivery(ctx)
	if err != nil || !ok {
		t.Fatalf("claim delivery: ok=%v err=%v", ok, err)
	}
	service.deliverQueued(ctx, job)
	if calls != 1 || !signatureValid {
		t.Fatalf("calls %d signatureValid %v, want one attempt with a valid V2 signature", calls, signatureValid)
	}
	// A transient failure goes back to the store instead of being retried in
	// place: the row keeps its attempt count with the lease released.
	var rescheduled bool
	if err := database.TestSQL().QueryRowContext(ctx,
		`SELECT attempts = 1 AND locked_at IS NULL FROM webhook_delivery_queue WHERE id = $1`, job.ID).Scan(&rescheduled); err != nil {
		t.Fatalf("read rescheduled delivery: %v", err)
	}
	if !rescheduled {
		t.Fatal("transient failure did not hand the delivery back to the store")
	}
	if _, err := database.TestSQL().ExecContext(ctx,
		`UPDATE webhook_delivery_queue SET available_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-time.Minute).Format(appmodel.CanonicalTimestampLayout), job.ID); err != nil {
		t.Fatal(err)
	}
	job, ok, err = database.ClaimWebhookDelivery(ctx)
	if err != nil || !ok {
		t.Fatalf("claim retry: ok=%v err=%v", ok, err)
	}
	service.deliverQueued(ctx, job)
	if calls != 2 || !signatureValid {
		t.Fatalf("calls %d signatureValid %v, want the retry to sign and deliver", calls, signatureValid)
	}
	deliveries, err := database.ListWebhookDeliveries(ctx, 1, hookSummary.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[int]int, len(deliveries))
	for _, delivery := range deliveries {
		statuses[delivery.Status]++
	}
	if statuses[http.StatusServiceUnavailable] != 1 || statuses[http.StatusOK] != 1 {
		t.Fatalf("delivery log statuses = %v, want one 503 and one 200", statuses)
	}
}
