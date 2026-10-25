package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A personal event is seen by its author and the managers only; a shared event is
// seen by the whole workspace; participants see it too.
func TestCalendarVisibilityAndParticipants(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "calendar-visibility")
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

	personal, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Title: "Личная встреча", StartsAt: start, EndsAt: start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("member creates a personal event: %v", err)
	}
	shared, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Планёрка", StartsAt: start.Add(24 * time.Hour), EndsAt: start.Add(25 * time.Hour), Shared: true,
	})
	if err != nil {
		t.Fatalf("manager creates a shared event: %v", err)
	}
	window := appmodel.CalendarEventListQuery{TeamID: ws.teamID, From: start.Add(-time.Hour), To: start.Add(72 * time.Hour)}

	seen := func(caller int64) map[int64]bool {
		t.Helper()
		q := window
		q.CallerID = caller
		events, err := d.ListCalendarEvents(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, e := range events {
			out[e.ID] = true
		}
		return out
	}

	if !seen(ws.memberID)[personal.ID] {
		t.Fatal("the author does not see its own event")
	}
	if !seen(ws.ownerID)[personal.ID] {
		t.Fatal("a manager does not see a personal event of a member")
	}
	if !seen(ws.memberID)[shared.ID] {
		t.Fatal("a member does not see the shared event")
	}

	inviteOnly, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Созвон с участником", StartsAt: start.Add(48 * time.Hour), EndsAt: start.Add(49 * time.Hour),
		ParticipantIDs: []int64{ws.memberID},
	})
	if err != nil {
		t.Fatalf("manager adds a member as participant: %v", err)
	}
	if !seen(ws.memberID)[inviteOnly.ID] {
		t.Fatal("a participant does not see the event it is listed on")
	}
}

// An end that is not after the start is refused; a participant from another
// workspace is refused; a member cannot delete someone else's event.
func TestCalendarRulesAndDeletion(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "calendar-rules")
	other := seedRichWorkspace(t, d, "calendar-rules-other")
	start := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	if _, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Наоборот", StartsAt: start, EndsAt: start.Add(-time.Minute),
	}); !errors.Is(err, model.ErrEventEndBeforeStart) {
		t.Fatalf("end before start = %v, want ErrEventEndBeforeStart", err)
	}
	if _, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Чужой", StartsAt: start, EndsAt: start.Add(time.Hour),
		ParticipantIDs: []int64{other.ownerID},
	}); !errors.Is(err, model.ErrCalendarTargetOutOfTeam) {
		t.Fatalf("participant from another workspace = %v, want ErrCalendarTargetOutOfTeam", err)
	}

	event, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Сдача", StartsAt: start, EndsAt: start.Add(time.Hour), Shared: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCalendarEvent(t.Context(), appmodel.CalendarEventDeleteRequest{TeamID: ws.teamID, CallerID: ws.memberID, EventID: event.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member deletes a colleague's event = %v, want ErrForbidden", err)
	}
	if err := d.DeleteCalendarEvent(t.Context(), appmodel.CalendarEventDeleteRequest{TeamID: ws.teamID, CallerID: ws.ownerID, EventID: event.ID}); err != nil {
		t.Fatalf("manager deletes the event: %v", err)
	}
}

// An event that starts before midnight and ends after it is listed on both days it
// touches, and not on a day it never reaches.
func TestCalendarEventAcrossMidnightIsOnBothDays(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "calendar-midnight")
	start := time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)
	if _, err := d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Через полночь", StartsAt: start, EndsAt: start.Add(2 * time.Hour), Shared: true,
	}); err != nil {
		t.Fatal(err)
	}
	onDay := func(day time.Time) int {
		t.Helper()
		events, err := d.ListCalendarEvents(t.Context(), appmodel.CalendarEventListQuery{
			TeamID: ws.teamID, CallerID: ws.memberID, From: day, To: day.AddDate(0, 0, 1),
		})
		if err != nil {
			t.Fatal(err)
		}
		return len(events)
	}
	if n := onDay(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)); n != 1 {
		t.Fatalf("the 26th shows %d events, want the one that starts that night", n)
	}
	if n := onDay(time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)); n != 1 {
		t.Fatalf("the 27th shows %d events, want the one that runs into it", n)
	}
	if n := onDay(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)); n != 0 {
		t.Fatalf("the 28th shows %d events, want none", n)
	}
}
