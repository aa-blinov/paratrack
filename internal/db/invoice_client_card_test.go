package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A draft changes the client's card only when the manager asks for it, and
// an empty field never clears what the card already holds: the card serves
// every project of that client.
func TestDraftEditUpdatesClientCardOnlyWhenAsked(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Карточка", "client-card")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	var clientID, projectID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name, email, details) VALUES (?, 'Ромашка', 'old@romashka.test', 'ИНН 7707083893, договор №12') RETURNING id`, teamID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name, slug, client_id) VALUES (?, 'Сайт', 'card-site', ?) RETURNING id`, teamID, clientID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-970", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	card := func() (name, email, details string) {
		t.Helper()
		if err := d.TestSQL().QueryRowContext(ctx, `SELECT name, email, details FROM clients WHERE id = ?`, clientID).Scan(&name, &email, &details); err != nil {
			t.Fatal(err)
		}
		return
	}
	edit := func(email, details string, updateCard bool) {
		t.Helper()
		if err := d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
			TeamID: teamID, InvoiceID: inv.ID, ProjectID: projectID, CallerID: ownerID,
			Client: "Ромашка", Email: email, Details: details, UpdateClientCard: updateCard,
		}); err != nil {
			t.Fatalf("edit: %v", err)
		}
	}

	edit("new@romashka.test", "ИНН 500100732259", false)
	if _, email, details := card(); email != "old@romashka.test" || details != "ИНН 7707083893, договор №12" {
		t.Fatalf("the card changed without being asked: %q %q", email, details)
	}
	edit("new@romashka.test", "", true)
	if _, email, details := card(); email != "new@romashka.test" || details != "ИНН 7707083893, договор №12" {
		t.Fatalf("asked, with empty details: %q %q", email, details)
	}
	// An invoice over every project has no card to update: refused, not skipped.
	if err := d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
		TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Client: "Ромашка", UpdateClientCard: true,
	}); !errors.Is(err, model.ErrNoClientCard) {
		t.Fatalf("card update on an invoice with no project: err = %v", err)
	}
	edit("", "ИНН 500100732259", true)
	if _, email, details := card(); email != "new@romashka.test" || details != "ИНН 500100732259" {
		t.Fatalf("asked, with new details and no email: %q %q", email, details)
	}
}
