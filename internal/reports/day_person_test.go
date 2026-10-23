package reports

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A day-by-person report keeps each person's time on a day apart, in calendar order (PLAN-07).
func TestDayPersonRowsSplitEachDayByMember(t *testing.T) {
	monday := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	tuesday := monday.AddDate(0, 0, 1)
	session := func(userID int64, start time.Time, seconds int) appmodel.ActiveSession {
		return appmodel.ActiveSession{
			Session:  model.Session{UserID: userID, StartAt: start, EndAt: timePtr(start.Add(time.Duration(seconds) * time.Second)), AccumulatedSeconds: seconds},
			Activity: model.Activity{ID: 1, Name: "дизайн", ProjectID: 1},
		}
	}
	input := AggregateInput{
		Sessions: []appmodel.ActiveSession{
			session(2, tuesday, 1800),
			session(1, monday, 3600),
			session(2, monday, 900),
		},
		UserNames:      map[int64]string{1: "Анна", 2: "Борис"},
		UnassignedUser: "без участника",
		GroupBy:        appmodel.ReportGroupDayPerson,
		From:           monday.Add(-time.Hour),
		To:             tuesday.Add(2 * time.Hour),
		Now:            tuesday.Add(2 * time.Hour),
	}

	result, err := Aggregate(input)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("%d rows, want one per day and person (3)", len(result.Rows))
	}
	want := []struct {
		day, person string
		seconds     int
	}{
		{"2026-09-07", "Анна", 3600},
		{"2026-09-07", "Борис", 900},
		{"2026-09-08", "Борис", 1800},
	}
	for i, w := range want {
		row := result.Rows[i]
		if row.Key != w.day || row.Person != w.person || row.Seconds != w.seconds {
			t.Fatalf("row %d = %+v, want %s %s %d seconds", i, row, w.day, w.person, w.seconds)
		}
	}
}
