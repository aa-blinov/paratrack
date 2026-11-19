package reminders

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// members are the people with no time on the day asked about; dayClaims keys team:user:day.
	members   []int64
	dayClaims map[string]bool
	zone      *time.Location
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
func (f *fakeStore) ManagerIDs(context.Context, int64) ([]int64, error)             { return f.managers, nil }
func (f *fakeStore) TasksDueOn(context.Context, string) ([]appmodel.DueTask, error) { return nil, nil }
func (f *fakeStore) MarkTaskDueReminded(context.Context, appmodel.TaskDueReminderRequest) error {
	return nil
}
func (f *fakeStore) TeamIDs(context.Context) ([]int64, error)                        { return f.teams, nil }
func (f *fakeStore) CRMStepsDue(context.Context, string) ([]appmodel.CRMStep, error) { return nil, nil }
func (f *fakeStore) ClaimCRMReminder(context.Context, appmodel.CRMReminderClaimRequest) (bool, error) {
	return false, nil
}
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

func (f *fakeStore) DayFillCandidates(context.Context, appmodel.DayFillCandidatesQuery) ([]int64, error) {
	return f.members, nil
}

func (f *fakeStore) ClaimDayFillNotice(_ context.Context, request appmodel.DayFillNoticeRequest) (bool, error) {
	if f.dayClaims == nil {
		f.dayClaims = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%d:%s", request.TeamID, request.UserID, request.Day)
	if f.dayClaims[key] {
		return false, nil
	}
	f.dayClaims[key] = true
	return true, nil
}

func (f *fakeStore) TeamLocation(context.Context, int64) (*time.Location, error) {
	if f.zone == nil {
		return time.UTC, nil
	}
	return f.zone, nil
}

type funcLogger struct{ logf func(string, ...any) }

func (l funcLogger) Printf(format string, args ...any) { l.logf(format, args...) }

// failingDeadlinesStore breaks only the deadlines read; the task, overload and
// day sections still answer.
type failingDeadlinesStore struct {
	*fakeStore
	tasks []appmodel.DueTask
}

func (f *failingDeadlinesStore) ProjectsDueBetween(context.Context, string, string) ([]appmodel.DueProject, error) {
	return nil, errors.New("the deadlines table is unreachable")
}

func (f *failingDeadlinesStore) TasksDueOn(context.Context, string) ([]appmodel.DueTask, error) {
	return f.tasks, nil
}

// One failing section must not silence the rest: a broken deadlines read used
// to end the pass before the task nudges ran, and the swallowed error left no
// trace of why every nudge had stopped.
func TestAFailingSectionDoesNotStopTheOthers(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	base := &fakeStore{managers: []int64{1}, teams: []int64{1}, claimed: map[string]bool{}}
	store := &failingDeadlinesStore{fakeStore: base, tasks: []appmodel.DueTask{
		{TeamID: 1, TaskID: 9, AssigneeID: 5, Title: "Правки", Due: "2026-11-02"},
	}}
	rec := &recorder{}
	var logged []string
	service, err := New(Dependencies{
		Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now },
		Logger: funcLogger{func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = service.Pass(context.Background())
	if err == nil || !strings.Contains(err.Error(), "deadline reminders") {
		t.Fatalf("pass error = %v, want the deadline failure named in it", err)
	}
	if len(rec.sent) != 1 || rec.sent[0].Topic != TopicTaskDue {
		t.Fatalf("the failing deadlines section silenced the task nudge: %+v", rec.sent)
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "deadline reminders") {
		t.Fatalf("the failure left no log line: %q", logged)
	}
}

// The fill-your-day nudge goes out once per person on a weekday evening in the
// workspace zone. Not in the morning, not at the weekend, not twice the same day.
func TestDayFillNudgesOncePerWeekdayEvening(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{teams: []int64{1}, members: []int64{5, 6}, claimed: map[string]bool{}, zone: tokyo}
	rec := &recorder{}
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC) // Wed 19:00 in Tokyo
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := service.Pass(ctx); err != nil {
		t.Fatalf("evening pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("evening pass sent %d notifications, want one per member (2)", len(rec.sent))
	}
	for i, want := range []int64{5, 6} {
		got := rec.sent[i]
		if got.Topic != TopicDayFill || len(got.UserIDs) != 1 || got.UserIDs[0] != want {
			t.Fatalf("notification %d = %+v, want the day fill nudge to member %d", i, got, want)
		}
	}

	if err := service.Pass(ctx); err != nil {
		t.Fatalf("second evening pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("second pass sent again: %d notifications in total, want still 2", len(rec.sent))
	}

	morning := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) // Wed 09:00 in Tokyo
	service.now = func() time.Time { return morning }
	store.dayClaims = map[string]bool{}
	if err := service.Pass(ctx); err != nil {
		t.Fatalf("morning pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("a morning pass sent a day fill nudge: %d in total", len(rec.sent))
	}

	saturday := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) // Sat 19:00 in Tokyo
	service.now = func() time.Time { return saturday }
	if err := service.Pass(ctx); err != nil {
		t.Fatalf("weekend pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("a weekend pass sent a day fill nudge: %d in total", len(rec.sent))
	}
}

type recordingMailer struct{ sent []Notification }

func (m *recordingMailer) Email(_ context.Context, n Notification) error {
	m.sent = append(m.sent, n)
	return nil
}

// The fill-your-day nudge goes by push and by email, the same nudge in both, and only in the window.
func TestDayFillAlsoGoesByEmail(t *testing.T) {
	store := &fakeStore{teams: []int64{1}, members: []int64{5}, claimed: map[string]bool{}}
	rec := &recorder{}
	mail := &recordingMailer{}
	now := time.Date(2026, 11, 11, 19, 0, 0, 0, time.UTC) // Wed 19:00, workspace zone UTC
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Mailer: mail, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("evening pass: %v", err)
	}
	if len(rec.sent) != 1 || len(mail.sent) != 1 {
		t.Fatalf("push %d, email %d; want one of each", len(rec.sent), len(mail.sent))
	}
	if mail.sent[0].Topic != TopicDayFill || mail.sent[0].Body != rec.sent[0].Body {
		t.Fatalf("email = %+v, want the same day fill nudge as the push", mail.sent[0])
	}
}
