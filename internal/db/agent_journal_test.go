package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A settled request stays in the journal with its outcome: a rejection, a confirmation that
// ran, and a confirmation the action refused, with the reason. A request still waiting is
// not in the journal; the queue shows it.
func TestAgentActionJournalKeepsTheOutcome(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "agent-journal")
	ctx := t.Context()
	ask := func(reason string) appmodel.AgentActionView {
		t.Helper()
		view, err := d.CreateAgentAction(ctx, appmodel.AgentActionCreateRequest{
			TeamID: ws.teamID, CallerID: ws.ownerID, Kind: appmodel.AgentActionWorkspaceDelete, TargetID: ws.teamID, Reason: reason,
		})
		if err != nil {
			t.Fatalf("request %q: %v", reason, err)
		}
		return view
	}
	query := appmodel.AgentActionListQuery{TeamID: ws.teamID, CallerID: ws.ownerID}

	ask("ждёт решения")
	rejected := ask("отказ")
	if _, err := d.DecideAgentAction(ctx, appmodel.AgentActionDecisionRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ActionID: rejected.ID, Reject: true,
	}); err != nil {
		t.Fatal(err)
	}
	failed := ask("не выполнится")
	claimed, err := d.DecideAgentAction(ctx, appmodel.AgentActionDecisionRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ActionID: failed.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.FinishAgentAction(ctx, appmodel.AgentActionFinishRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ActionID: failed.ID, ClaimedAt: claimed.DecidedAt,
		Status: "failed", Error: "В пространстве есть другие участники",
	}); err != nil {
		t.Fatal(err)
	}

	journal, err := d.ListAgentActionJournal(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal) != 2 {
		t.Fatalf("journal has %d rows, want the rejection and the failure only: %+v", len(journal), journal)
	}
	byID := map[int64]appmodel.AgentActionView{}
	for _, row := range journal {
		byID[row.ID] = row
	}
	if got := byID[failed.ID]; got.Status != "failed" || got.Error != "В пространстве есть другие участники" {
		t.Fatalf("failed request = status %q, error %q; want failed with the reason", got.Status, got.Error)
	}
	if got := byID[rejected.ID]; got.Status != "rejected" || got.Error != "" {
		t.Fatalf("rejected request = status %q, error %q; want rejected with no error", got.Status, got.Error)
	}
}
