package reports

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestSummarizeStatsBuildsActivityAndProjectBreakdowns(t *testing.T) {
	start := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	end := func(seconds int) *time.Time {
		value := start.Add(time.Duration(seconds) * time.Second)
		return &value
	}
	sessions := []model.ActiveSession{
		{Session: model.Session{ID: 1, StartAt: start, EndAt: end(3600), AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "Build", ProjectID: 7}},
		{Session: model.Session{ID: 2, StartAt: start, EndAt: end(1800), AccumulatedSeconds: 1800}, Activity: model.Activity{Name: "Build", ProjectID: 7}},
		{Session: model.Session{ID: 3, StartAt: start, EndAt: end(1800), AccumulatedSeconds: 1800}, Activity: model.Activity{Name: "Review"}},
	}
	projects := map[int64]model.Project{
		7: {ID: 7, Name: "Product", Slug: "product", Color: "#123456"},
	}
	to := start.Add(time.Hour)
	summary, err := SummarizeStats(sessions, projects, start, to, to, "Uncategorized")
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalSeconds != 7200 {
		t.Fatalf("total seconds = %d, want 7200", summary.TotalSeconds)
	}
	if len(summary.Activities) != 2 || summary.Activities[0].Name != "Build" || summary.Activities[0].Seconds != 5400 || summary.Activities[0].Share != 75 {
		t.Fatalf("unexpected activity breakdown: %#v", summary.Activities)
	}
	if len(summary.Projects) != 2 {
		t.Fatalf("project count = %d, want 2", len(summary.Projects))
	}
	product := summary.Projects[0]
	if product.ID != 7 || product.Name != "Product" || product.Seconds != 5400 || product.Share != 75 {
		t.Fatalf("unexpected product summary: %#v", product)
	}
	if len(product.Activities) != 1 || product.Activities[0].Name != "Build" || product.Activities[0].Seconds != 5400 {
		t.Fatalf("unexpected product activities: %#v", product.Activities)
	}
	uncategorized := summary.Projects[1]
	if uncategorized.ID != 0 || uncategorized.Name != "Uncategorized" || uncategorized.Seconds != 1800 || uncategorized.Share != 25 {
		t.Fatalf("unexpected uncategorized summary: %#v", uncategorized)
	}
}
