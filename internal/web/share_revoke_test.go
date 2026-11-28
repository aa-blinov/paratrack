package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A client link opens the invoice until it is revoked; after that it is not
// found, the same as an expired one.
func TestRevokedClientLinkStopsOpening(t *testing.T) {
	e := newAPIEnv(t)
	e.register("share-revoke@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"freelance"}, "from": {"welcome"}}, nil).Body.Close()
	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT m.team_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'share-revoke@x.test'`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-601", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddInvoiceLine(ctx, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Label: "Работа", AmountCents: 100000}); err != nil {
		t.Fatal(err)
	}
	if err := e.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	path := "/api/invoices/" + itoa64(inv.ID) + "/share"
	body := readBody(t, e.do("POST", path, url.Values{}, map[string]string{"Accept": "application/json"}))
	var link struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &link); err != nil || link.URL == "" {
		t.Fatalf("share: %v %s", err, body)
	}
	sharePath := link.URL[strings.Index(link.URL, "/share/"):]
	open := func() int {
		resp := e.do("GET", sharePath, nil, nil)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := open(); code != http.StatusOK {
		t.Fatalf("a fresh link: %d", code)
	}
	revoke := readBody(t, e.do("DELETE", path, nil, map[string]string{"Accept": "application/json"}))
	var revoked struct {
		Revoked int64 `json:"revoked"`
	}
	if err := json.Unmarshal([]byte(revoke), &revoked); err != nil || revoked.Revoked != 1 {
		t.Fatalf("revoke: %v %s", err, revoke)
	}
	if code := open(); code != http.StatusNotFound {
		t.Fatalf("a revoked link: %d", code)
	}
}

func itoa64(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
