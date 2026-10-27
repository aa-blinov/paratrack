package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// An all-day event covers whole days: it is stored as midnight of its first day and midnight
// after its last, it is never pushed to a provider, and it turns back into a timed event.
func TestAllDayEventCoversWholeDays(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "calendar-all-day")
	ctx := t.Context()
	first := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 0, 2)

	event, err := d.CreateCalendarEvent(ctx, appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Выездной день", StartsAt: first, EndsAt: last, Shared: true, AllDay: true,
	})
	if err != nil {
		t.Fatalf("manager creates an all-day event: %v", err)
	}
	if !event.AllDay || !event.StartsAt.Equal(first) || !event.EndsAt.Equal(last) {
		t.Fatalf("all-day event = all day %v, %v to %v; want whole days from %v to %v", event.AllDay, event.StartsAt, event.EndsAt, first, last)
	}

	if _, err := d.CreateCalendarEvent(ctx, appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Не в провайдер", StartsAt: first, EndsAt: last, AllDay: true, SyncPush: true,
	}); fieldOf(err) != "sync_push" {
		t.Fatalf("all-day event pushed to a provider = %v, want field sync_push", err)
	}
	if _, err := d.CreateCalendarEvent(ctx, appmodel.CalendarEventCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Пустой", StartsAt: first, EndsAt: first, AllDay: true,
	}); !errors.Is(err, model.ErrEventEndBeforeStart) {
		t.Fatalf("all-day event ending on its own start = %v, want ErrEventEndBeforeStart", err)
	}

	notAllDay := false
	timed, err := d.UpdateCalendarEvent(ctx, appmodel.CalendarEventUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, EventID: event.ID, AllDay: &notAllDay,
		StartsAt: timePtr(first.Add(9 * time.Hour)), EndsAt: timePtr(first.Add(10 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("all-day event turns timed: %v", err)
	}
	if timed.AllDay || !timed.StartsAt.Equal(first.Add(9*time.Hour)) {
		t.Fatalf("timed event = all day %v, starts %v; want a timed event at 09:00", timed.AllDay, timed.StartsAt)
	}
}

func timePtr(value time.Time) *time.Time { return &value }

// The contact of a client is changed by its creator or a manager, the rule that removes it.
// A name must stay a name, and a field left out keeps its value.
func TestCRMContactEditFollowsTheCreatorRule(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-contact-edit")
	ctx := t.Context()

	byOwner, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ClientID: ws.clientID, Name: "Анна", Role: "Директор", Phone: "+7 900 000-00-00",
	})
	if err != nil {
		t.Fatal(err)
	}
	newName := "Анна Смирнова"
	if _, err := d.UpdateCRMContact(ctx, appmodel.CRMContactUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byOwner.ID, Name: &newName,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member edits the manager's contact = %v, want ErrForbidden", err)
	}

	byMember, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Name: "Олег",
	})
	if err != nil {
		t.Fatal(err)
	}
	empty := "   "
	if _, err := d.UpdateCRMContact(ctx, appmodel.CRMContactUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byMember.ID, Name: &empty,
	}); fieldOf(err) != "contact name" {
		t.Fatalf("empty name = %v, want field contact name", err)
	}
	role := "Бухгалтер"
	edited, err := d.UpdateCRMContact(ctx, appmodel.CRMContactUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byMember.ID, Role: &role,
	})
	if err != nil {
		t.Fatalf("creator edits own contact: %v", err)
	}
	if edited.Name != "Олег" || edited.Role != "Бухгалтер" {
		t.Fatalf("edited contact = name %q, role %q; want the name kept and the role changed", edited.Name, edited.Role)
	}

	if _, err := d.UpdateCRMContact(ctx, appmodel.CRMContactUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ContactID: byMember.ID, Name: &newName,
	}); err != nil {
		t.Fatalf("manager edits a member's contact: %v", err)
	}
}

// A session is recorded after it has happened: an end later than now is refused.
func TestBackfillRefusesASessionThatHasNotEnded(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "backfill-future")
	ctx := requestctx.WithActor(t.Context(), ws.ownerID)
	future := d.currentTime().Add(time.Hour)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{
		TeamID: ws.teamID, ActivityID: ws.activityID, Start: future, End: future.Add(time.Hour),
	}); !errors.Is(err, model.ErrSessionEndInFuture) {
		t.Fatalf("session ending in the future = %v, want ErrSessionEndInFuture", err)
	}
}
