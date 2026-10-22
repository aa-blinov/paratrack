package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A member marks the start of their day once and the end after it, and a stranger
// cannot mark anyone's day (TIME-06).
func TestWorkdayStartsOnceAndEndsAfterIt(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "День", "workday-marks")
	memberID := seedProjectTestUser(t, d, "workday-member")
	addMember(t, d, teamID, memberID, "member")
	outsiderID := seedProjectTestUser(t, d, "workday-outsider")
	morning := time.Date(2026, 10, 20, 9, 12, 0, 0, time.UTC)
	evening := time.Date(2026, 10, 20, 18, 40, 0, 0, time.UTC)

	if err := d.MarkWorkday(ctx, appmodel.WorkdayRequest{TeamID: teamID, UserID: outsiderID, Day: "2026-10-20", Action: "start", Now: morning}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a stranger started a workday: %v", err)
	}
	if err := d.MarkWorkday(ctx, appmodel.WorkdayRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Action: "end", Now: evening}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a day ended before it started: %v", err)
	}
	if err := d.MarkWorkday(ctx, appmodel.WorkdayRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Action: "start", Now: morning}); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkWorkday(ctx, appmodel.WorkdayRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Action: "start", Now: evening}); err != nil {
		t.Fatalf("a repeated start is refused, not ignored: %v", err)
	}
	if err := d.MarkWorkday(ctx, appmodel.WorkdayRequest{TeamID: teamID, UserID: memberID, Day: "2026-10-20", Action: "end", Now: evening}); err != nil {
		t.Fatal(err)
	}

	mark, ok, err := d.WorkdayOf(ctx, teamID, memberID, "2026-10-20")
	if err != nil || !ok {
		t.Fatalf("the day reads back as %v (err %v)", ok, err)
	}
	if !mark.StartedAt.Equal(morning) || !mark.EndedAt.Equal(evening) {
		t.Fatalf("the day is %v to %v, want the first start and the end", mark.StartedAt, mark.EndedAt)
	}
}
