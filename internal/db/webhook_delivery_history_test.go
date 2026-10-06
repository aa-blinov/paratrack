package db

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The settings screen can only help an integrator debug if the body that went
// out is in the history, and if a long one cannot grow the row without bound.
func TestDeliveryHistoryKeepsATruncatedRequestBody(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Delivery bodies", "delivery-bodies")
	ownerID := teamOwner(t, d, teamID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "*",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"event":"session.stopped","team_id":1,"data":{"session_id":1}}` + strings.Repeat("я", 5000)
	if err := d.LogWebhookDelivery(ctx, appmodel.WebhookDeliveryLogRequest{
		WebhookID: hook.ID, Event: "session.stopped", Status: 200, RequestBody: body,
	}); err != nil {
		t.Fatal(err)
	}
	deliveries, err := d.ListWebhookDeliveries(ctx, teamID, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want the logged attempt", len(deliveries))
	}
	if len(deliveries[0].RequestBody) > deliveryBodyLimit || len(deliveries[0].RequestBody) < deliveryBodyLimit-2 || deliveries[0].RequestBody == body {
		t.Fatalf("stored request body is %d bytes, want a %d byte prefix", len(deliveries[0].RequestBody), deliveryBodyLimit)
	}
	if !strings.HasPrefix(deliveries[0].RequestBody, `{"event":"session.stopped"`) {
		t.Fatalf("stored body starts with %q, want the readable beginning of the request", deliveries[0].RequestBody[:32])
	}
	if strings.Contains(deliveries[0].RequestBody, "signing-secret") {
		t.Fatal("delivery history kept the signing secret")
	}
	if deliveries[0].Status != 200 || deliveries[0].CreatedAt.IsZero() {
		t.Fatalf("delivery = %+v, want the status and time preserved", deliveries[0])
	}
}

// A body cut in the middle of a multi-byte rune would leave invalid UTF-8 in
// the settings screen.
func TestDeliveryBodyTruncationKeepsWholeRunes(t *testing.T) {
	if text, truncated := truncateWebhookBody("short body"); text != "short body" || truncated {
		t.Fatalf("truncateWebhookBody(short) = (%q, %v), want it untouched", text, truncated)
	}
	body := "x" + strings.Repeat("я", deliveryBodyLimit)
	text, truncated := truncateWebhookBody(body)
	if !truncated || !utf8.ValidString(text) || !strings.HasPrefix(text, "xяяя") || len(text) >= len(body) {
		t.Fatalf("truncateWebhookBody(multibyte) = (%d bytes, valid=%v, truncated=%v), want a whole-rune prefix",
			len(text), utf8.ValidString(text), truncated)
	}
}

// Once the response columns exist, the answer has to come back with the attempt:
// this is the same history read against the migrated table.
func TestDeliveryHistoryKeepsTheResponseBodyAfterMigration(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Delivery answers", "delivery-answers")
	ownerID := teamOwner(t, d, teamID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "*",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE webhook_deliveries ADD COLUMN request_truncated BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE webhook_deliveries ADD COLUMN response TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE webhook_deliveries ADD COLUMN response_truncated BIGINT NOT NULL DEFAULT 0`,
	} {
		if _, err := d.TestSQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.LogWebhookDelivery(ctx, appmodel.WebhookDeliveryLogRequest{
		WebhookID: hook.ID, Event: "session.stopped", Status: 200,
		RequestBody: `{"event":"session.stopped","action":"test"}`, ResponseBody: `{"received":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	recent, err := d.ListRecentWebhookDeliveries(ctx, appmodel.WebhookDeliveryHistoryQuery{TeamID: teamID, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	attempts := recent[hook.ID]
	if len(attempts) != 1 {
		t.Fatalf("recent attempts = %d, want the logged one", len(attempts))
	}
	if attempts[0].RequestBody != `{"event":"session.stopped","action":"test"}` || attempts[0].ResponseBody != `{"received":true}` {
		t.Fatalf("attempt bodies = (%q, %q), want the request and the answer", attempts[0].RequestBody, attempts[0].ResponseBody)
	}
	if attempts[0].BodyTruncated {
		t.Fatal("short bodies must not be marked as truncated")
	}
	// A long answer stays bounded, and says so.
	if err := d.LogWebhookDelivery(ctx, appmodel.WebhookDeliveryLogRequest{
		WebhookID: hook.ID, Event: "session.stopped", Status: 500,
		ResponseBody: strings.Repeat("x", deliveryBodyLimit+500),
	}); err != nil {
		t.Fatal(err)
	}
	deliveries, err := d.ListWebhookDeliveries(ctx, teamID, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries[0].ResponseBody) != deliveryBodyLimit || !deliveries[0].BodyTruncated {
		t.Fatalf("long answer stored as %d bytes (truncated=%v), want a %d byte prefix flagged as cut",
			len(deliveries[0].ResponseBody), deliveries[0].BodyTruncated, deliveryBodyLimit)
	}
}

// The signing secret leaves persistence only for a manager of that workspace.
func TestGetWebhookForDeliveryRejectsNonManager(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Test run scope", "test-run-scope")
	ownerID := teamOwner(t, d, teamID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "signing-secret", Events: "*",
	})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "webhook-test-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetWebhookForDelivery(ctx, appmodel.WebhookLookupCommand{TeamID: teamID, CallerID: memberID, WebhookID: hook.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member endpoint lookup = %v, want forbidden", err)
	}
	if _, err := d.GetWebhookForDelivery(ctx, appmodel.WebhookLookupCommand{TeamID: teamID, CallerID: ownerID, WebhookID: hook.ID + 999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown endpoint lookup = %v, want not found", err)
	}
	loaded, err := d.GetWebhookForDelivery(ctx, appmodel.WebhookLookupCommand{TeamID: teamID, CallerID: ownerID, WebhookID: hook.ID})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Secret != "signing-secret" || loaded.URL != hook.URL || loaded.Events != "*" {
		t.Fatalf("loaded endpoint = %+v, want the endpoint with its signing secret", loaded)
	}
}
