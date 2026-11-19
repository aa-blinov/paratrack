package reports

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The task report (UX-16) answers "what did this task cost": time on an
// activity that belongs to a task lands in the task's bucket at its project's
// rate, and time on a free activity keeps the activity's own name, so the
// report loses nothing.
func TestAggregateByTask(t *testing.T) {
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	now := to
	rate := 25000
	project := model.Project{ID: 7, Name: "Клиентский портал", Billable: true}
	project.BillableRateCents = &rate
	session := func(activityID int64, name string, seconds int) appmodel.ActiveSession {
		start := from.Add(9 * time.Hour)
		end := start.Add(time.Duration(seconds) * time.Second)
		return appmodel.ActiveSession{
			Activity: model.Activity{ID: activityID, Name: name, ProjectID: 7},
			Session: model.Session{
				UserID: 1, StartAt: start, EndAt: &end,
				AccumulatedSeconds: seconds,
			},
		}
	}
	input := AggregateInput{
		Sessions: []appmodel.ActiveSession{
			session(11, "Вёрстка карточки", 7200),  // task "Каталог": 2h
			session(12, "Правки текстов", 3600),    // task "Каталог": 1h
			session(13, "Созвон с командой", 1800), // free activity: 0.5h
		},
		Projects:      map[int64]model.Project{7: project},
		ActivityTasks: map[int64]string{11: "Каталог", 12: "Каталог"},
		TeamCurrency:  "RUB",
		GroupBy:       appmodel.ReportGroupTask,
		Billable:      true,
		From:          from,
		To:            to,
		Now:           now,
		Location:      time.UTC,
	}

	result, err := Aggregate(input)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (the task and the free activity)", len(result.Rows))
	}
	byLabel := map[string]AggregateRow{}
	for _, row := range result.Rows {
		byLabel[row.Key] = row
	}
	if catalog, ok := byLabel["Каталог"]; !ok {
		t.Fatalf("no bucket for the task: %+v", result.Rows)
	} else {
		if catalog.Seconds != 3*3600 {
			t.Fatalf("task seconds = %d, want 3h", catalog.Seconds)
		}
		// 3 h × 250.00 = 750.00.
		if catalog.AmountCents != 3*rate {
			t.Fatalf("task amount = %d, want %d", catalog.AmountCents, 3*rate)
		}
		if catalog.Project != project.Name {
			t.Fatalf("task project = %q, want %q", catalog.Project, project.Name)
		}
	}
	if call, ok := byLabel["Созвон с командой"]; !ok {
		t.Fatalf("free activity lost its own bucket: %+v", result.Rows)
	} else if call.Seconds != 1800 {
		t.Fatalf("free activity seconds = %d, want 1800", call.Seconds)
	}
	if result.TotalSeconds != 3*3600+1800 {
		t.Fatalf("total seconds = %d", result.TotalSeconds)
	}
}
