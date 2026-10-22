package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A client's link opens the invoice until it expires, a manager makes it, and only the
// hash of the token is stored (FIN-13).
func TestInvoiceShareLinkOpensUntilItExpires(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Ссылка", "invoice-share-link")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "share-member")
	addMember(t, d, teamID, memberID, "member")
	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-SHARE', 'Client', '2026-10-01T00:00:00Z', '2026-10-08T00:00:00Z', 'sent', '2026-10-08T09:00:00Z') RETURNING id`,
		teamID).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("client-token"))
	hash := hex.EncodeToString(sum[:])
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	request := appmodel.InvoiceShareRequest{TeamID: teamID, InvoiceID: invoiceID, CallerID: memberID, TokenHash: hash, Now: now, ExpiresAt: now.Add(time.Hour)}

	if err := d.CreateInvoiceShareLink(ctx, request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member made a client link: %v", err)
	}
	request.CallerID = ownerID
	if err := d.CreateInvoiceShareLink(ctx, request); err != nil {
		t.Fatal(err)
	}

	link, err := d.FindInvoiceShareLink(ctx, appmodel.InvoiceShareLookupQuery{TokenHash: hash, Now: now.Add(30 * time.Minute)})
	if err != nil || link.InvoiceID != invoiceID || link.TeamID != teamID {
		t.Fatalf("the live link opens %+v (err %v), want the invoice", link, err)
	}
	if _, err := d.FindInvoiceShareLink(ctx, appmodel.InvoiceShareLookupQuery{TokenHash: hash, Now: now.Add(2 * time.Hour)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired link still opens: %v", err)
	}

	var plain int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM invoice_share_links WHERE token_hash = 'client-token'`).Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if plain != 0 {
		t.Fatal("the token itself was stored")
	}
}
