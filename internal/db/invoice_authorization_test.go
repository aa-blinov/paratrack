package db

import (
	"database/sql"
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestInvoiceWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-001", Client: "Client", Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "P", Slug: "p", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	denied := []struct {
		name string
		call func() error
	}{
		{"legacy-create", func() error {
			_, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: memberID, Number: "INV-2026-002", Client: "Other", Start: start, End: end})
			return err
		}},
		{"draft-create", func() error {
			_, _, err := d.CreateInvoiceDraft(ctx, appmodel.InvoiceDraftRequest{
				TeamID: teamID, CallerID: memberID, Client: "Client", Start: start, End: end,
			})
			return err
		}},
		{"mark-sent", func() error {
			err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID})
			return err
		}},
		{"mark-paid", func() error {
			_, _, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID})
			return err
		}},
		{"delete", func() error {
			return d.DeleteInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID})
		}},
		{"payment-link", func() error {
			return d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{
				TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID, ExpectedRevision: invoice.Revision, PaymentURL: "https://pay.example",
			})
		}},
		{"rebuild", func() error {
			return d.RebuildInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID})
		}},
		{"edit", func() error {
			return d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
				TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID, Client: "Other",
			})
		}},
		{"receipt", func() error {
			return d.SetInvoiceReceipt(ctx, appmodel.InvoiceReceiptRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: memberID, Receipt: "receipt"})
		}},
		{"assign-history", func() error {
			return d.AssignUnassignedActivityForBilling(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: project.ID, CallerID: memberID})
		}},
	}
	for _, test := range denied {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, model.ErrForbidden) {
				t.Fatalf("write error = %v, want forbidden", err)
			}
		})
	}

	got, err := d.GetInvoice(ctx, teamID, invoice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "draft" || got.ClientName != "Client" {
		t.Errorf("unauthorized writes changed invoice: status=%q client=%q", got.Status, got.ClientName)
	}
}

func TestCreateInvoiceCannotBillSessionsFromAnotherWorkspace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "Workspace A", "workspace-a")
	ownerA := teamOwner(t, d, teamA)
	teamB := seedTeam(t, d, "Workspace B", "workspace-b")
	ownerB := teamOwner(t, d, teamB)

	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamB, CallerID: ownerB, Name: "private work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	session, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamB, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamA, CallerID: ownerA, Number: "INV-CROSS-TEAM", Client: "Client", Start: start, End: start.AddDate(0, 0, 1), Lines: []InvoiceLine{{
		Label: "forged line", Seconds: 3600, RateCents: 1000, AmountCents: 1000, SessionIDs: []int64{session.ID},
	}}})
	if !errors.Is(err, ErrAlreadyBilled) {
		t.Fatalf("cross-workspace session error = %v, want %v", err, ErrAlreadyBilled)
	}

	var invoiceID sql.NullInt64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, session.ID).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	if invoiceID.Valid {
		t.Fatalf("workspace B session was linked to workspace A invoice %d", invoiceID.Int64)
	}
}
