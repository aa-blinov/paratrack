package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The currency, the invoice rules, Stripe and webhooks are the owner's alone, even for an
// admin who manages everything else. The route refuses it, and so does the transaction
// behind the route (ADM-15).
func TestOnlyTheOwnerChangesCurrencyRulesStripeAndWebhooks(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("owner-only@x.test")
	admin := newAPIEnvSharedDB(t, owner)
	admin.register("admin-only@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"admin-only@x.test": admin}, "admin-only@x.test")

	var adminID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'admin-only@x.test'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	readBody(t, owner.do("POST", fmt.Sprintf("/api/team/members/%d/role", adminID), url.Values{"role": {"admin"}}, nil))

	asJSON := map[string]string{"Accept": "application/json"}
	refused := map[string]*http.Response{
		"currency": admin.do("POST", "/api/team/currency", url.Values{"currency": {"EUR"}}, nil),
		"billing":  admin.do("POST", "/api/team/billing", url.Values{"round_minutes": {"15"}, "round_mode": {"up"}}, nil),
		"stripe":   admin.do("POST", "/api/team/stripe", url.Values{"key": {"sk_test_x"}}, nil),
		"webhook":  admin.do("POST", "/api/webhooks", url.Values{"url": {"https://example.com/h"}, "secret": {"s"}, "events": {"*"}}, asJSON),
	}
	for name, resp := range refused {
		readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("an admin changed the %s: status %d, want 403", name, resp.StatusCode)
		}
	}

	accepted := owner.do("POST", "/api/team/currency", url.Values{"currency": {"EUR"}}, nil)
	readBody(t, accepted)
	if accepted.StatusCode == http.StatusForbidden {
		t.Fatal("the owner was refused the currency")
	}

	// The transaction refuses the admin without the route in front of it.
	// The workspace the owner shares with the admin, not the admin's own personal one.
	var teamID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT t.id FROM teams t JOIN users u ON u.id = t.owner_id WHERE u.email = 'owner-only@x.test'`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	err := owner.srv.services.TeamOps.UpdateCurrency(context.Background(), appmodel.TeamCurrencyRequest{TeamID: teamID, CallerID: adminID, Currency: "USD"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("the transaction let an admin change the currency: %v", err)
	}
}
