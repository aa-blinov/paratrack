package web

import (
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A sent invoice keeps the executors' names hidden as it went out: turning
// the workspace switch off afterwards does not put them back on the document.
func TestSentInvoiceKeepsTheNameHidingItWasSentWith(t *testing.T) {
	e := newAPIEnv(t)
	e.register("hide-frozen@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))
	var ownerID, teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT user_id, team_id FROM memberships WHERE role = 'owner' ORDER BY team_id LIMIT 1`).Scan(&ownerID, &teamID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-FROZEN-1", Client: "Acme", Start: start, End: start.Add(2 * time.Hour),
		Lines: []model.InvoiceLine{{Label: "Acme · consulting · U", Seconds: 7200, RateCents: 10000, AmountCents: 20000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE invoices SET by_person = 1 WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE teams SET hide_executor_names = 1 WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE teams SET hide_executor_names = 0 WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	text, failed := callTool(t, e, auth, "invoice_get", map[string]any{"invoice_id": inv.ID})
	if failed {
		t.Fatalf("invoice_get failed: %s", text)
	}
	if strings.Contains(text, "consulting · U") || !strings.Contains(text, `"Acme · consulting"`) {
		t.Fatalf("the sent invoice took the new setting: %s", text)
	}
}
