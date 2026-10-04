package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestWebhookWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	hook, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "secret", Events: "*"})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "webhook-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: memberID, URL: "https://example.test/other", Secret: "secret", Events: "*"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member create error = %v, want forbidden", err)
	}
	if err := d.DeleteWebhook(ctx, appmodel.WebhookDeleteRequest{TeamID: teamID, CallerID: memberID, WebhookID: hook.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member delete error = %v, want forbidden", err)
	}
	hooks, err := d.ListWebhooks(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || hooks[0].ID != hook.ID {
		t.Fatalf("unauthorized webhook writes changed endpoints: %+v", hooks)
	}
}
