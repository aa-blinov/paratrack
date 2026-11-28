package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/model"
)

// An import is refused whole when one of its entries starts in a closed month.
func TestImportIsRefusedIntoAClosedMonth(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "closed-import")
	ownerID := teamOwner(t, d, teamID)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO closed_periods (team_id, month, closed_at, closed_by, closed_by_role) VALUES (?, '2026-09-01', ?, ?, 'owner')`,
		teamID, FormatTime(time.Now().UTC()), ownerID); err != nil {
		t.Fatal(err)
	}
	open := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	closed := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	_, err := d.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: ownerID, Entries: []importport.ImportedEntry{
		{Activity: "A", Start: open, End: open.Add(time.Hour)},
		{Activity: "A", Start: closed, End: closed.Add(time.Hour)},
	}})
	var periodErr *model.PeriodClosedError
	if !errors.As(err, &periodErr) {
		t.Fatalf("import into a closed month: %v", err)
	}
	var count int
	d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE team_id = ?`, teamID).Scan(&count)
	if count != 0 {
		t.Fatalf("a refused import wrote %d sessions", count)
	}
}
