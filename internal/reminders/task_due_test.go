package reminders

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A task due tomorrow reminds its assignee once, and a task already overdue
// reminds once more; a second pass on the same day sends nothing new (TASK-05).
func TestTaskDueNudgesTheAssigneeOncePerDueDate(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	store := perPersonClaims{
		fakeStore: &fakeStore{managers: []int64{1}, teams: []int64{1}, claimed: map[string]bool{}},
		seen:      map[int64]bool{},
		tasks: []appmodel.DueTask{
			{TeamID: 1, TaskID: 9, Title: "Бриф", Due: "2026-11-03", AssigneeID: 5},
			{TeamID: 1, TaskID: 10, Title: "Отчёт", Due: "2026-11-01", AssigneeID: 6},
		},
	}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Mailer: &failingMailer{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("first pass sent %d task nudges, want 2 (one soon, one overdue)", len(rec.sent))
	}
	soon, overdue := rec.sent[0], rec.sent[1]
	if soon.Topic != TopicTaskDue || len(soon.UserIDs) != 1 || soon.UserIDs[0] != 5 {
		t.Fatalf("soon nudge = %+v, want the assignee of the brief", soon)
	}
	if overdue.Title != "Задача просрочена" || overdue.UserIDs[0] != 6 {
		t.Fatalf("overdue nudge = %+v, want the assignee of the report", overdue)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("second pass on the same due dates sent %d more nudges", len(rec.sent)-2)
	}
}
