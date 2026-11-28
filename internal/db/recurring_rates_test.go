package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A project without a rate of its own is billed at the rates of the people on
// it, so the monthly schedule drafts it; a project where nothing carries a
// price is not drafted at all, because a document asking for zero is worse
// than no document.
func TestRecurringDraftNeedsAPriceSomewhere(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Расписание ставок", "recurring-rates")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	// Billable, and no rate of its own: the people price it.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE projects SET billable = 1, billable_rate_cents = NULL WHERE id = ?`, project.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id, project_id, archived, color, created_at, updated_at)
		 VALUES ('Вёрстка', ?, ?, 0, '#7c3aed', ?, ?) RETURNING id`,
		teamID, project.ID, FormatTime(start), FormatTime(start)).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	projectID := project.ID
	end := start.AddDate(0, 1, 0)
	seedClosedSession(t, d, teamID, ownerID, activityID, start.Add(9*time.Hour))

	draft := func() (Invoice, []InvoiceLine, error) {
		return d.CreateInvoiceDraft(ctx, appmodel.InvoiceDraftRequest{
			TeamID: teamID, CallerID: ownerID, Start: start, End: end,
			Options:           appmodel.InvoiceOptions{ProjectID: projectID},
			UnpricedIsNothing: true,
		})
	}
	if _, _, err := draft(); err == nil {
		t.Fatal("a draft was made for hours nobody priced")
	}
	// The owner's own hour for the client prices it.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE memberships SET bill_rate_cents = 250000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	invoice, lines, err := draft()
	if err != nil {
		t.Fatalf("draft at the member's rate: %v", err)
	}
	if len(lines) != 1 || lines[0].AmountCents != 250000 {
		t.Fatalf("lines = %+v, want one hour at the owner's rate", lines)
	}
	if invoice.ID == 0 {
		t.Fatal("the draft was not stored")
	}
}
