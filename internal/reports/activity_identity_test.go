package reports

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Two activities that share a name are two rows: an activity is its id, and the name is
// only a label (D-7, TIME-15). Each row names its project and the task it holds (TIME-16).
func TestActivityRowsStayApartByIdentity(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	input := AggregateInput{
		Sessions: []appmodel.ActiveSession{
			{Session: model.Session{ActivityID: 1, StartAt: start, EndAt: timePtr(start.Add(time.Hour)), AccumulatedSeconds: 3600}, Activity: model.Activity{ID: 1, Name: "созвон", ProjectID: 1}},
			{Session: model.Session{ActivityID: 2, StartAt: start, EndAt: timePtr(start.Add(30 * time.Minute)), AccumulatedSeconds: 1800}, Activity: model.Activity{ID: 2, Name: "созвон", ProjectID: 2}},
		},
		Projects:      map[int64]model.Project{1: {ID: 1, Name: "EORA"}, 2: {ID: 2, Name: "Ромашка"}},
		ActivityTasks: map[int64]string{1: "Сайт"},
		GroupBy:       appmodel.ReportGroupActivity,
		From:          start.Add(-time.Hour),
		To:            start.Add(2 * time.Hour),
		Now:           start.Add(2 * time.Hour),
	}

	result, err := Aggregate(input)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("%d rows for two activities that share a name, want 2", len(result.Rows))
	}
	first, second := result.Rows[0], result.Rows[1]
	if first.ActivityID != 1 || first.Seconds != 3600 || first.Project != "EORA" || first.Task != "Сайт" {
		t.Fatalf("first row %+v, want activity 1 with its project and task", first)
	}
	if second.ActivityID != 2 || second.Seconds != 1800 || second.Project != "Ромашка" || second.Task != "" {
		t.Fatalf("second row %+v, want activity 2 with its project and no task", second)
	}
	labels := appmodel.ActivityLabels(result.Rows)
	if labels[0] != "созвон (#1)" || labels[1] != "созвон (#2)" {
		t.Fatalf("labels %q, want the id after a shared name", labels)
	}
}

// A name that only one activity holds stays as it is.
func TestActivityLabelKeepsAUniqueNameAsItIs(t *testing.T) {
	labels := appmodel.ActivityLabels([]appmodel.ReportAggregateRow{{Key: "дизайн", ActivityID: 7}, {Key: "дизайн", ActivityID: 7, Currency: "EUR"}})
	if labels[0] != "дизайн" || labels[1] != "дизайн" {
		t.Fatalf("labels %q, want the plain name for one activity in two currencies", labels)
	}
}
