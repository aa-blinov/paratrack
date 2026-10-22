package reminders

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// failingMailer records each email nudge and can fail the first one.
type failingMailer struct {
	calls     []Notification
	failFirst bool
}

func (m *failingMailer) Email(_ context.Context, notification Notification) error {
	m.calls = append(m.calls, notification)
	if m.failFirst && len(m.calls) == 1 {
		return errors.New("smtp unavailable")
	}
	return nil
}

// perPersonClaims marks each person's overload notice once, as the store does;
// the shared fake marks the whole week, which would hold back the second person.
type perPersonClaims struct {
	*fakeStore
	seen map[int64]bool
}

func (p perPersonClaims) ClaimOverloadNotice(_ context.Context, request appmodel.OverloadNoticeRequest) (bool, error) {
	if p.seen[request.UserID] {
		return false, nil
	}
	p.seen[request.UserID] = true
	return true, nil
}

// An overload nudge goes by email too (TIME-14). One failed mail is reported,
// but it does not hold back the other people's nudges in the same pass.
func TestOverloadNudgeAlsoGoesByEmail(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	store := perPersonClaims{fakeStore: &fakeStore{managers: []int64{1}, teams: []int64{1}, claimed: map[string]bool{}}, seen: map[int64]bool{}}
	planner := fakePlanner{snapshot: appmodel.ScheduleSnapshot{Rows: []appmodel.ScheduleViewRow{
		{ScheduleRow: appmodel.ScheduleRow{UserID: 5, UserName: "Маша", Total: 600}, AvailableMinutes: 480},
		{ScheduleRow: appmodel.ScheduleRow{UserID: 6, UserName: "Петя", Total: 600}, AvailableMinutes: 480},
	}}}
	rec := &recorder{}
	mailer := &failingMailer{failFirst: true}
	service, err := New(Dependencies{Store: store, Planner: planner, Sender: rec, Mailer: mailer, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Pass(context.Background()); err == nil {
		t.Fatal("the failed mail was not reported")
	}
	if len(rec.sent) != 2 {
		t.Fatalf("push nudges = %d, want 2", len(rec.sent))
	}
	if len(mailer.calls) != 2 {
		t.Fatalf("mail calls = %d, want 2: one failure must not stop the other nudge", len(mailer.calls))
	}
	if mailer.calls[1].Topic != TopicOverload {
		t.Fatalf("mailed topic = %q, want %q", mailer.calls[1].Topic, TopicOverload)
	}
}
