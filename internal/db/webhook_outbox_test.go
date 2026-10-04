package db

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

func TestWebhookDeliveryQueueLeasesRetriesAndCompletes(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Webhook queue", "webhook-queue")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "invoice.paid"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	payload := []byte(`{"event":"invoice.paid","team_id":1,"data":{"invoice_id":7}}`)
	if err := d.EnqueueWebhookDeliveries(ctx, appmodel.WebhookDeliveryBatchRequest{TeamID: teamID, WebhookIDs: []int64{hook.ID}, Event: "invoice.paid", Payload: payload}); err != nil {
		t.Fatalf("EnqueueWebhookDeliveries: %v", err)
	}

	job, ok, err := d.ClaimWebhookDelivery(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookDelivery = (%+v, %v, %v), want one job", job, ok, err)
	}
	if job.TeamID != teamID || job.WebhookID != hook.ID || job.URL != hook.URL || job.Secret != "signing-secret" || job.Event != "invoice.paid" || job.Attempts != 1 || !bytes.Equal(job.Payload, payload) {
		t.Fatalf("claimed job = %+v", job)
	}
	if _, ok, err := d.ClaimWebhookDelivery(ctx); err != nil || ok {
		t.Fatalf("second claim before lease expiry = (%v, %v), want no job", ok, err)
	}

	if err := d.RetryWebhookDelivery(ctx, appmodel.WebhookDeliveryRetryRequest{Job: job, AvailableAt: time.Now().Add(-time.Second), MaxAttempts: 10}); err != nil {
		t.Fatalf("RetryWebhookDelivery: %v", err)
	}
	retried, ok, err := d.ClaimWebhookDelivery(ctx)
	if err != nil || !ok || retried.Attempts != 2 {
		t.Fatalf("claim after retry = (%+v, %v, %v), want attempt 2", retried, ok, err)
	}
	if err := d.CompleteWebhookDelivery(ctx, retried); err != nil {
		t.Fatalf("CompleteWebhookDelivery: %v", err)
	}
	if _, ok, err := d.ClaimWebhookDelivery(ctx); err != nil || ok {
		t.Fatalf("claim after completion = (%v, %v), want no job", ok, err)
	}
}

func TestStartSessionCommitsTypedWebhookEventToTransactionalOutbox(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Webhook event outbox", "webhook-event-outbox")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "Architecture"})
	if err != nil {
		t.Fatalf("getOrCreateActivity: %v", err)
	}
	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "session.started"}); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	startedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: startedAt})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	event, ok, err := d.ClaimWebhookEvent(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookEvent = (%+v, %v, %v), want one committed event", event, ok, err)
	}
	decoded, err := webhookport.DecodeEvent(webhookport.EventName(event.Event), event.Payload)
	started, typed := decoded.(*webhookport.SessionStartedEvent)
	if err != nil || !typed || started.SessionID != session.ID || started.Activity != activity.Name || len(event.WebhookIDs) != 1 {
		t.Fatalf("outbox event = %+v, decoded=%+v, err=%v", event, decoded, err)
	}
	if err := d.CompleteWebhookEvent(ctx, event); err != nil {
		t.Fatalf("CompleteWebhookEvent: %v", err)
	}
	if _, ok, err := d.ClaimWebhookEvent(ctx); err != nil || ok {
		t.Fatalf("ClaimWebhookEvent after completion = (%v, %v), want no event", ok, err)
	}
}

func TestWebhookFanoutReplayDoesNotDuplicateEndpointDelivery(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Webhook fanout replay", "webhook-fanout-replay")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "session.started"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	payload := []byte(`{"event":"session.started","team_id":1,"data":{"session_id":7}}`)
	const sourceEventID = 42
	for range 2 {
		if err := d.EnqueueWebhookDeliveries(ctx, appmodel.WebhookDeliveryBatchRequest{SourceEventID: sourceEventID, TeamID: teamID, WebhookIDs: []int64{hook.ID}, Event: "session.started", Payload: payload}); err != nil {
			t.Fatalf("EnqueueWebhookDeliveries: %v", err)
		}
	}
	job, ok, err := d.ClaimWebhookDelivery(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookDelivery = (%+v, %v, %v), want one job", job, ok, err)
	}
	if err := d.CompleteWebhookDelivery(ctx, job); err != nil {
		t.Fatalf("CompleteWebhookDelivery: %v", err)
	}
	if _, ok, err := d.ClaimWebhookDelivery(ctx); err != nil || ok {
		t.Fatalf("second ClaimWebhookDelivery = (%v, %v), want no duplicate", ok, err)
	}
}

func TestManualInvoicePaymentEmitsPaidEventExactlyOnce(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Invoice paid webhook", "invoice-paid-webhook")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "secret", Events: "invoice.paid"}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-PAID-EVENT", Client: "Client", Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	clockReads := 0
	d.now = func() time.Time {
		clockReads++
		return baseTime.Add(time.Duration(clockReads) * time.Second)
	}
	number, changed, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID})
	if err != nil || !changed || number != invoice.Number {
		t.Fatalf("MarkInvoicePaidOnce = (%q, %v, %v), want paid transition", number, changed, err)
	}
	if clockReads != 1 {
		t.Fatalf("payment transition read the clock %d times, want one stable timestamp", clockReads)
	}
	var paidAt, eventCreatedAt string
	if err := d.sql.QueryRowContext(ctx, `SELECT paid_at FROM invoices WHERE id = ?`, invoice.ID).Scan(&paidAt); err != nil {
		t.Fatal(err)
	}
	if err := d.sql.QueryRowContext(ctx, `SELECT created_at FROM webhook_event_outbox WHERE team_id = ? AND event = 'invoice.paid'`, teamID).Scan(&eventCreatedAt); err != nil {
		t.Fatal(err)
	}
	if paidAt != eventCreatedAt || paidAt != FormatTime(baseTime.Add(time.Second)) {
		t.Fatalf("paid_at=%q event created_at=%q; want one timestamp %q", paidAt, eventCreatedAt, FormatTime(baseTime.Add(time.Second)))
	}
	event, ok, err := d.ClaimWebhookEvent(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookEvent = (%+v, %v, %v), want paid event", event, ok, err)
	}
	decoded, err := webhookport.DecodeEvent(webhookport.EventName(event.Event), event.Payload)
	paid, typed := decoded.(*webhookport.InvoicePaidEvent)
	if err != nil || !typed || paid.InvoiceID != invoice.ID || paid.TeamID != teamID || len(event.WebhookIDs) != 1 {
		t.Fatalf("outbox event = %+v, decoded=%+v, err=%v", event, decoded, err)
	}
	if err := d.CompleteWebhookEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil || changed {
		t.Fatalf("repeated payment = (changed=%v, err=%v), want idempotent no-op", changed, err)
	}
	if _, ok, err := d.ClaimWebhookEvent(ctx); err != nil || ok {
		t.Fatalf("repeated payment queued another event = (%v, %v), want none", ok, err)
	}
}

func TestStripeInvoicePaymentRequiresMatchingSessionAndEmitsOnce(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Stripe paid webhook", "stripe-paid-webhook")
	ownerID := teamOwner(t, d, teamID)
	otherTeamID := seedTeam(t, d, "Stripe other workspace", "stripe-other-workspace")
	otherOwnerID := teamOwner(t, d, otherTeamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "secret", Events: "invoice.paid"}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-STRIPE-PAID", Client: "Client", Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	const oldSessionID = "cs_test_old_workspace_scoped"
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, ExpectedRevision: invoice.Revision, PaymentURL: "https://checkout.stripe.com/old-session", StripeSessionID: oldSessionID}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetInvoiceReceipt(ctx, appmodel.InvoiceReceiptRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, Receipt: "receipt-1"}); err != nil {
		t.Fatal(err)
	}
	const sessionID = "cs_test_current_workspace_scoped"
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, ExpectedRevision: invoice.Revision, PaymentURL: "https://checkout.stripe.com/stale-session", StripeSessionID: sessionID}); !errors.Is(err, ErrInvoiceChanged) {
		t.Fatalf("stale checkout write error = %v, want invoice changed", err)
	}
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, ExpectedRevision: invoice.Revision + 1, PaymentURL: "https://checkout.stripe.com/current-session", StripeSessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	otherInvoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: otherTeamID, CallerID: otherOwnerID, Number: "INV-STRIPE-OTHER", Client: "Client", Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if paid, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{TeamID: otherTeamID, InvoiceID: otherInvoice.ID, StripeSessionID: sessionID}); !errors.Is(err, ErrStripeSessionMismatch) || paid {
		t.Fatalf("cross-workspace payment = (%v, %v), want mismatch", paid, err)
	}
	if paid, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{TeamID: teamID, InvoiceID: invoice.ID, StripeSessionID: oldSessionID}); err != nil || !paid {
		t.Fatalf("payment from earlier checkout of unchanged invoice = (%v, %v), want payment", paid, err)
	}
	if paid, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{TeamID: teamID, InvoiceID: invoice.ID, StripeSessionID: sessionID}); err != nil || paid {
		t.Fatalf("payment from second checkout after invoice was paid = (%v, %v), want idempotent no-op", paid, err)
	}
	event, ok, err := d.ClaimWebhookEvent(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookEvent = (%+v, %v, %v), want Stripe paid event", event, ok, err)
	}
	decoded, err := webhookport.DecodeEvent(webhookport.EventName(event.Event), event.Payload)
	paidEvent, typed := decoded.(*webhookport.InvoicePaidEvent)
	if err != nil || !typed || paidEvent.InvoiceID != invoice.ID || paidEvent.TeamID != teamID || len(event.WebhookIDs) != 1 {
		t.Fatalf("outbox event = %+v, decoded=%+v, err=%v", event, decoded, err)
	}
	if err := d.CompleteWebhookEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if paid, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{TeamID: teamID, InvoiceID: invoice.ID, StripeSessionID: oldSessionID}); err != nil || paid {
		t.Fatalf("repeated Stripe payment = (%v, %v), want idempotent no-op", paid, err)
	}
	if _, ok, err := d.ClaimWebhookEvent(ctx); err != nil || ok {
		t.Fatalf("repeated Stripe payment queued another event = (%v, %v), want none", ok, err)
	}
}

func TestRebuildingInvoiceInvalidatesItsStripeCheckout(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Rebuilt Stripe invoice", "rebuilt-stripe-invoice")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-STRIPE-REBUILD", Client: "Client", Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "cs_test_before_rebuild"
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, ExpectedRevision: invoice.Revision, PaymentURL: "https://checkout.stripe.com/before-rebuild", StripeSessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	if err := d.RebuildInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := d.GetInvoice(ctx, teamID, invoice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.PaymentURL != "" {
		t.Fatalf("payment URL after rebuild = %q, want empty", rebuilt.PaymentURL)
	}
	if paid, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{TeamID: teamID, InvoiceID: invoice.ID, StripeSessionID: sessionID}); !errors.Is(err, ErrStripeSessionMismatch) || paid {
		t.Fatalf("payment from pre-rebuild checkout = (%v, %v), want mismatch", paid, err)
	}
}
