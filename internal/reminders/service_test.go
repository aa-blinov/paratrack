package reminders

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type fakeStore struct {
	due      []appmodel.DueProject
	reminded []string
	claimed  map[string]bool
	managers []int64
	teams    []int64
}

func (f *fakeStore) ProjectsDueBetween(_ context.Context, _, _ string) ([]appmodel.DueProject, error) {
	out := f.due
	f.due = nil // a reminded project is no longer due
	return out, nil
}
func (f *fakeStore) MarkDeadlineReminded(_ context.Context, request appmodel.DeadlineReminderRequest) error {
	f.reminded = append(f.reminded, request.Deadline)
	return nil
}
func (f *fakeStore) ManagerIDs(context.Context, int64) ([]int64, error) { return f.managers, nil }
func (f *fakeStore) TeamIDs(context.Context) ([]int64, error)           { return f.teams, nil }
func (f *fakeStore) ClaimOverloadNotice(_ context.Context, request appmodel.OverloadNoticeRequest) (bool, error) {
	key := request.Week
	if f.claimed[key] {
		return false, nil
	}
	f.claimed[key] = true
	return true, nil
}

type fakePlanner struct{ snapshot appmodel.ScheduleSnapshot }

func (f fakePlanner) List(context.Context, appmodel.ScheduleQuery) (appmodel.ScheduleSnapshot, error) {
	return f.snapshot, nil
}

type recorder struct{ sent []Notification }

func (r *recorder) Send(_ context.Context, n Notification) error {
	r.sent = append(r.sent, n)
	return nil
}

// A deadline reminder goes to the managers and the lead, once. An overload
// reminder goes to the managers, once per person per week.
func TestPassSendsEachNudgeOnce(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{
		due:      []appmodel.DueProject{{TeamID: 1, ProjectID: 7, Name: "Сайт", Slug: "site", Deadline: "2026-11-04", LeadUserID: 5}},
		managers: []int64{1},
		teams:    []int64{1},
		claimed:  map[string]bool{},
	}
	planner := fakePlanner{snapshot: appmodel.ScheduleSnapshot{Rows: []appmodel.ScheduleViewRow{
		{ScheduleRow: appmodel.ScheduleRow{UserID: 5, UserName: "Маша", Total: 600}, AvailableMinutes: 480},
	}}}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: planner, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("first pass sent %d notifications, want 2 (deadline and overload)", len(rec.sent))
	}
	deadline := rec.sent[0]
	if deadline.Topic != TopicDeadline || len(deadline.UserIDs) != 2 {
		t.Fatalf("deadline reminder = %+v, want to the manager and the lead", deadline)
	}
	if len(store.reminded) != 1 || store.reminded[0] != "2026-11-04" {
		t.Fatalf("deadline not marked reminded: %v", store.reminded)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("second pass sent again: %d notifications in total, want still 2", len(rec.sent))
	}
}
