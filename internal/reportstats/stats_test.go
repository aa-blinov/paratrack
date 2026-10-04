package reportstats

import (
	"math"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestSummarizeTotalsSharesAndStableOrdering(t *testing.T) {
	start := time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	active := []model.ActiveSession{
		sessionForStats("Alpha", 2, start, end, 3600),
		sessionForStats("Zulu", 2, start, end, 1800),
		sessionForStats("Alpha", 2, start, end, 1800),
		sessionForStats("Misc", 0, start, end, 1800),
	}
	projects := map[int64]model.Project{
		2: {ID: 2, Name: "Client work", Slug: "client-work", Color: "#123456"},
	}

	got, err := Summarize(active, projects, start, end, end, "Uncategorized")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalSeconds != 9000 {
		t.Fatalf("TotalSeconds = %d, want 9000", got.TotalSeconds)
	}
	if len(got.Activities) != 3 {
		t.Fatalf("activity count = %d, want 3", len(got.Activities))
	}
	if got.Activities[0].Name != "Alpha" || got.Activities[0].Seconds != 5400 || got.Activities[0].Share != 60 {
		t.Fatalf("first activity = %#v, want Alpha at 5400 seconds and 60%%", got.Activities[0])
	}
	if got.Activities[1].Name != "Misc" || got.Activities[2].Name != "Zulu" {
		t.Fatalf("tied activities = %q, %q, want alphabetical order", got.Activities[1].Name, got.Activities[2].Name)
	}
	if len(got.Projects) != 2 || got.Projects[0].ID != 2 || got.Projects[1].ID != 0 {
		t.Fatalf("project order = %#v, want project 2 then uncategorized", got.Projects)
	}
	project := got.Projects[0]
	if project.Name != "Client work" || project.Slug != "client-work" || project.Color != "#123456" || project.Seconds != 7200 {
		t.Fatalf("project summary = %#v", project)
	}
	if math.Abs(project.Share-80) > 0.000001 {
		t.Fatalf("project share = %v, want 80%%", project.Share)
	}
	if got.Projects[1].Name != "Uncategorized" || got.Projects[1].Color != "#9ca3af" || math.Abs(got.Projects[1].Share-20) > 0.000001 {
		t.Fatalf("uncategorized summary = %#v", got.Projects[1])
	}
	if len(project.Activities) != 2 || project.Activities[0].Name != "Alpha" || project.Activities[1].Name != "Zulu" {
		t.Fatalf("project activities = %#v, want Alpha then Zulu", project.Activities)
	}
}

func TestSummarizeEmptyInputHasNoBreakdowns(t *testing.T) {
	got, err := Summarize(nil, nil, time.Time{}, time.Time{}, time.Time{}, "Uncategorized")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalSeconds != 0 || len(got.Activities) != 0 || len(got.Projects) != 0 {
		t.Fatalf("empty summary = %#v", got)
	}
}

func TestHourlyGraphSplitsTrackedMinutesAcrossHourBuckets(t *testing.T) {
	location := time.FixedZone("user", 2*60*60)
	from := time.Date(2026, time.June, 1, 0, 0, 0, 0, location)
	start := time.Date(2026, time.June, 1, 10, 30, 0, 0, location)
	end := time.Date(2026, time.June, 1, 13, 45, 0, 0, location)
	tracked := int(end.Sub(start).Seconds())

	got, err := HourlyGraph([]model.ActiveSession{
		sessionForStats("Focus", 0, start, end, tracked),
	}, from, from.AddDate(0, 0, 1), end)
	if err != nil {
		t.Fatalf("HourlyGraph() error = %v", err)
	}
	if got.TotalSeconds != tracked || len(got.Series) != 1 {
		t.Fatalf("HourlyGraph() total/series = %d/%d", got.TotalSeconds, len(got.Series))
	}
	series := got.Series[0]
	if series.Name != "Focus" || series.TotalMinutes != 195 {
		t.Fatalf("series summary = %#v", series)
	}
	for hour, want := range map[int]int{10: 30, 11: 60, 12: 60, 13: 45} {
		if series.HourMinutes[hour] != want {
			t.Errorf("hour %d = %d minutes, want %d", hour, series.HourMinutes[hour], want)
		}
	}
}

func TestHourlyGraphUsesLocalHourAndStableSeriesOrder(t *testing.T) {
	location := time.FixedZone("user", 3*60*60)
	from := time.Date(2026, time.June, 1, 0, 0, 0, 0, location)
	start := time.Date(2026, time.June, 1, 14, 0, 0, 0, location)
	end := start.Add(time.Hour)
	sessions := []model.ActiveSession{
		sessionForStats("Zulu", 0, start, end, 3600),
		sessionForStats("Alpha", 0, start, end, 3600),
	}

	got, err := HourlyGraph(sessions, from, from.AddDate(0, 0, 1), end)
	if err != nil {
		t.Fatalf("HourlyGraph() error = %v", err)
	}
	if len(got.Series) != 2 || got.Series[0].Name != "Alpha" || got.Series[1].Name != "Zulu" {
		t.Fatalf("series ordering = %#v, want Alpha then Zulu", got.Series)
	}
	if got.Series[0].HourMinutes[14] != 60 {
		t.Fatalf("local hour bucket = %d, want 60 minutes at hour 14", got.Series[0].HourMinutes[14])
	}
}

func sessionForStats(activity string, projectID int64, start, end time.Time, seconds int) model.ActiveSession {
	return model.ActiveSession{
		Session:  model.Session{StartAt: start, EndAt: &end, AccumulatedSeconds: seconds},
		Activity: model.Activity{Name: activity, ProjectID: projectID},
	}
}
