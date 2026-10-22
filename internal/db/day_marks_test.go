package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A member marks their own day as overloaded or idle, the team sees it, and clearing it
// removes it. Someone outside the workspace cannot mark a day in it (TIME-13).
func TestDayMarkIsOwnAndClearable(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Отметки", "day-marks")
	memberID := seedProjectTestUser(t, d, "mark-member")
	addMember(t, d, teamID, memberID, "member")
	outsiderID := seedProjectTestUser(t, d, "mark-outsider")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	if err := d.SetDayMark(ctx, appmodel.DayMarkRequest{TeamID: teamID, UserID: outsiderID, Day: "2026-10-20", Kind: "overload", Now: now}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a stranger marked a day in the workspace: %v", err)
	}
	if err := d.SetDayMark(ctx, appmodel.DayMarkRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Kind: "overload", Note: "релиз", Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetDayMark(ctx, appmodel.DayMarkRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Kind: "idle", Now: now}); err != nil {
		t.Fatal(err)
	}

	marks, err := d.DayMarks(ctx, teamID, "2026-10-19", "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].UserID != memberID || marks[0].Kind != "idle" {
		t.Fatalf("the marks are %+v, want the member's one day, changed to idle", marks)
	}

	if err := d.SetDayMark(ctx, appmodel.DayMarkRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Kind: "", Now: now}); err != nil {
		t.Fatal(err)
	}
	if marks, _ := d.DayMarks(ctx, teamID, "2026-10-19", "2026-10-26"); len(marks) != 0 {
		t.Fatalf("a cleared mark is still there: %+v", marks)
	}
}
