package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The cell only raises a day. A start and an end the person logged stay; the sheet
// session takes the difference; a total below the logged intervals is refused and
// changes nothing.
func TestTimesheetCellKeepsLoggedIntervalsAndOnlyRaisesTheDay(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "cell-intervals", "cell-intervals")
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "cell intervals"})
	if err != nil {
		t.Fatal(err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	logged := day.Add(10 * time.Hour)
	if _, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: logged, End: logged.Add(time.Hour), Note: "logged",
	}); err != nil {
		t.Fatalf("logged interval: %v", err)
	}
	dayTotal := func() int {
		t.Helper()
		var secs int
		if err := d.TestSQL().QueryRowContext(ctx,
			`SELECT COALESCE(SUM(accumulated_seconds), 0) FROM sessions WHERE activity_id = ? AND user_id = ? AND start_at >= ? AND start_at < ?`,
			activity.ID, ownerID, FormatTime(day), FormatTime(day.AddDate(0, 0, 1))).Scan(&secs); err != nil {
			t.Fatal(err)
		}
		return secs
	}
	loggedCount := func() int {
		t.Helper()
		var count int
		if err := d.TestSQL().QueryRowContext(ctx,
			`SELECT count(*) FROM sessions WHERE activity_id = ? AND user_id = ? AND note = 'logged'`, activity.ID, ownerID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	set := func(seconds int) error {
		return d.UpsertDayTotal(ownerCtx, appmodel.TimesheetCellUpdateRequest{TeamID: teamID, ActivityID: activity.ID, Day: day, TotalSeconds: seconds})
	}

	// The cell raises the day: the logged hour stays, the sheet adds the rest.
	if err := set(5400); err != nil {
		t.Fatalf("raise to 90 min: %v", err)
	}
	if got := dayTotal(); got != 5400 || loggedCount() != 1 {
		t.Fatalf("after raise: total %d, logged intervals %d; want 5400 and the logged interval kept", got, loggedCount())
	}

	// Below the logged hour the cell is refused and the day stays as it was.
	if err := set(3000); !errors.Is(err, model.ErrCellBelowIntervals) {
		t.Fatalf("cell below the logged hour = %v, want ErrCellBelowIntervals", err)
	}
	if got := dayTotal(); got != 5400 || loggedCount() != 1 {
		t.Fatalf("refused write changed the day: total %d, logged intervals %d", got, loggedCount())
	}

	// Clearing a day that holds a logged interval is refused too.
	if err := set(0); !errors.Is(err, model.ErrCellBelowIntervals) {
		t.Fatalf("clear over a logged interval = %v, want ErrCellBelowIntervals", err)
	}

	// Raising again replaces only the sheet session.
	if err := set(7200); err != nil {
		t.Fatalf("raise to 2 h: %v", err)
	}
	if got := dayTotal(); got != 7200 || loggedCount() != 1 {
		t.Fatalf("after second raise: total %d, logged intervals %d; want 7200 and the logged interval kept", got, loggedCount())
	}
}
