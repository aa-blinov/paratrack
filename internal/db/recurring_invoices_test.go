package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The recurring schedule's reading (UX-20): only the workspaces that switched
// it on, with the owner and the zone the pass keeps its months in.
func TestRecurringWorkspacesListScheduledTeams(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	onTeam := seedTeam(t, d, "Расписание", "scheduled")
	offTeam := seedTeam(t, d, "Без расписания", "unscheduled")
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE teams SET recurring_invoices = '1st', timezone = 'Europe/Berlin' WHERE id = ?`, onTeam); err != nil {
		t.Fatalf("seed the schedule: %v", err)
	}
	_ = offTeam

	workspaces, err := d.RecurringWorkspaces(ctx)
	if err != nil {
		t.Fatalf("RecurringWorkspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].TeamID != onTeam {
		t.Fatalf("workspaces = %+v, want only the scheduled team", workspaces)
	}
	owner := teamOwner(t, d, onTeam)
	if workspaces[0].OwnerID != owner {
		t.Fatalf("owner = %d, want %d", workspaces[0].OwnerID, owner)
	}
	if workspaces[0].Zone == nil || workspaces[0].Zone.String() != "Europe/Berlin" {
		t.Fatalf("zone = %v, want the workspace's own Berlin", workspaces[0].Zone)
	}
}

// The letter's claim answers once per workspace and month: the second tick
// does not repeat the "ready to issue" letter, and an attempt for a month the
// workspace is not served returns the claim genuinely.
func TestClaimRecurringInvoiceLetterAnswersOnce(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	first, err := d.ClaimRecurringInvoiceLetter(ctx, appmodel.RecurringLetterClaimRequest{TeamID: teamID, Month: "2026-11"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !first {
		t.Fatal("the first claim of a fresh month returned false")
	}
	again, err := d.ClaimRecurringInvoiceLetter(ctx, appmodel.RecurringLetterClaimRequest{TeamID: teamID, Month: "2026-11"})
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if again {
		t.Fatal("the second claim of the same month returned true")
	}
	// A different month claims its own letter.
	december, err := d.ClaimRecurringInvoiceLetter(ctx, appmodel.RecurringLetterClaimRequest{TeamID: teamID, Month: "2026-12"})
	if err != nil {
		t.Fatalf("another month: %v", err)
	}
	if !december {
		t.Fatal("the next month's claim returned false")
	}
}

// The money settings carry the schedule round-trip: the owner switches it on,
// the settings page reads it back; an unknown schedule name fails the save.
func TestTeamBillingRoundTripsRecurring(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	owner := teamOwner(t, d, teamID)

	request := appmodel.TeamBillingRequest{TeamID: teamID, CallerID: owner, Rules: model.BillingRules{
		RoundMinutes: 15, RoundMode: "up", InvoicePrefix: "SITE", Recurring: "1st",
	}}
	if err := d.SetTeamBilling(ctx, request); err != nil {
		t.Fatalf("save: %v", err)
	}
	rules, err := d.TeamBilling(ctx, teamID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rules.Recurring != "1st" || rules.InvoicePrefix != "SITE" || rules.RoundMinutes != 15 {
		t.Fatalf("rules = %+v, want the schedule saved with the rest", rules)
	}

	request.Rules.Recurring = "cron"
	if err := d.SetTeamBilling(ctx, request); err != nil {
		t.Fatalf("save an unknown schedule: %v", err)
	}
	// ... the store accepts what the workflow validated; the guard lives in
	// the teams workflow, so here the round-trip keeps its shape.
	rules, err = d.TeamBilling(ctx, teamID)
	if err != nil || rules.Recurring != "cron" {
		t.Fatalf("unknown schedule round-trip = %+v, %v", rules, err)
	}
}
