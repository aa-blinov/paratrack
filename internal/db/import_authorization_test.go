package db

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/importport"

	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestImportEntries_RequiresCurrentWorkspaceMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "import-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	entry := importport.ImportedEntry{Activity: "Imported", Start: start, End: start.Add(time.Hour)}
	if _, err := d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: memberID, Entries: []importport.ImportedEntry{entry}}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member import error = %v, want forbidden", err)
	}
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE team_id = ?`, teamID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unauthorized import wrote %d sessions", count)
	}

	if result, err := d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: ownerID, Entries: []importport.ImportedEntry{entry}}); err != nil || result.Imported != 1 {
		t.Fatalf("owner import result = %+v, error = %v", result, err)
	}
	var importedUser int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE team_id = ?`, teamID).Scan(&importedUser); err != nil {
		t.Fatal(err)
	}
	if importedUser != ownerID {
		t.Fatalf("imported session user = %d, want actor %d", importedUser, ownerID)
	}
}

func TestImportEntriesBatchesActivityResolutionAndDeduplicatesExternalIDs(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Batch import", "batch-import")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	entries := []importport.ImportedEntry{
		{ExternalID: "ext-1", Activity: "Focus", Start: start, End: start.Add(20 * time.Minute)},
		{ExternalID: "ext-2", Activity: " focus ", Start: start.Add(time.Hour), End: start.Add(80 * time.Minute)},
		{ExternalID: "ext-1", Activity: "Must be skipped", Start: start.Add(2 * time.Hour), End: start.Add(140 * time.Minute)},
	}
	result, err := d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: ownerID, Entries: entries})
	if err != nil {
		t.Fatalf("ImportEntries: %v", err)
	}
	if result.Imported != 2 || result.Skipped != 1 {
		t.Fatalf("ImportEntries result = %+v, want imported=2 skipped=1", result)
	}
	rows, err := d.ListClosedSessions(ctx, appmodel.ClosedSessionsQuery{TeamID: teamID, Start: start, End: start.Add(3 * time.Hour)})
	if err != nil {
		t.Fatalf("ListClosedSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("imported sessions = %d, want 2", len(rows))
	}
	if rows[0].Activity.ID != rows[1].Activity.ID || rows[0].Activity.Name != "Focus" {
		t.Fatalf("import activity resolution = %+v / %+v, want one shared Focus activity", rows[0].Activity, rows[1].Activity)
	}
	result, err = d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: ownerID, Entries: entries[:2]})
	if err != nil {
		t.Fatalf("repeat ImportEntries: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 2 {
		t.Fatalf("repeat import result = %+v, want imported=0 skipped=2", result)
	}
}
