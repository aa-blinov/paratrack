package reports

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type graphSessionsStub struct {
	all       []model.ActiveSession
	projectID int64
}

func (s *graphSessionsStub) ClosedSessions(context.Context, int64, time.Time, time.Time, *int64) ([]model.ActiveSession, error) {
	return s.all, nil
}
func (s *graphSessionsStub) ClosedSessionsForProject(_ context.Context, _ int64, _, _ time.Time, projectID int64) ([]model.ActiveSession, error) {
	s.projectID = projectID
	return s.all, nil
}

type graphTagsStub struct {
	calls     int
	bySession map[int64][]model.Tag
}

func (*graphTagsStub) List(context.Context, int64) ([]model.Tag, error) { return nil, nil }
func (s *graphTagsStub) TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error) {
	s.calls++
	return s.bySession, nil
}

type graphProjectsStub struct{ project model.Project }

func (s graphProjectsStub) List(context.Context, int64, bool) ([]model.Project, error) {
	return []model.Project{s.project}, nil
}
func (s graphProjectsStub) GetBySlug(context.Context, int64, string) (model.Project, error) {
	return s.project, nil
}
func (graphProjectsStub) Currencies(context.Context, int64) (map[int64]string, error) {
	return nil, nil
}

func TestBuildGraphAppliesProjectAndTagFiltersBeforeAggregation(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := from.Add(time.Hour)
	reader := &graphSessionsStub{all: []model.ActiveSession{
		{Session: model.Session{ID: 1, StartAt: from, EndAt: &end, AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "Design", ProjectID: 7}},
		{Session: model.Session{ID: 2, StartAt: from, EndAt: &end, AccumulatedSeconds: 3600}, Activity: model.Activity{Name: "Build", ProjectID: 7}},
	}}
	tags := &graphTagsStub{bySession: map[int64][]model.Tag{1: {{Name: "urgent"}}, 2: {{Name: "later"}}}}
	builder := &Builder{sessions: reader, tags: tags, projects: graphProjectsStub{project: model.Project{ID: 7, Name: "Project", Slug: "project"}}}
	got, err := builder.BuildGraph(context.Background(), GraphQuery{
		TeamID: 4, From: from, To: from.AddDate(0, 0, 1), Now: from.Add(12 * time.Hour), ProjectSlug: "project", Tag: "urgent",
	})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if reader.projectID != 7 {
		t.Fatalf("project query id = %d, want 7", reader.projectID)
	}
	if tags.calls != 1 {
		t.Fatalf("tag lookup calls = %d, want 1", tags.calls)
	}
	if got.Project.ID != 7 {
		t.Fatalf("graph project = %#v, want project 7", got.Project)
	}
	if got.Graph.TotalSeconds != 3600 || len(got.Graph.Series) != 1 || got.Graph.Series[0].Name != "Design" {
		t.Fatalf("filtered graph buckets = %#v, want one 3600-second Design series", got.Graph)
	}
}

func TestBuildGraphSkipsTagReadWithoutTagFilter(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := from.Add(time.Hour)
	reader := &graphSessionsStub{all: []model.ActiveSession{{
		Session:  model.Session{ID: 1, StartAt: from, EndAt: &end, AccumulatedSeconds: 3600},
		Activity: model.Activity{Name: "Focus"},
	}}}
	tags := &graphTagsStub{}
	builder := &Builder{sessions: reader, tags: tags}
	got, err := builder.BuildGraph(context.Background(), GraphQuery{TeamID: 4, From: from, To: end, Now: end})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if len(got.Graph.Series) != 1 || tags.calls != 0 {
		t.Fatalf("series=%d tag reads=%d; want one series and no tag read", len(got.Graph.Series), tags.calls)
	}
}

func TestBuildStatsFiltersTaggedSessionsBeforeAggregation(t *testing.T) {
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	sessionEnd := start.Add(time.Hour)
	secondEnd := start.Add(3 * time.Hour)
	reader := &graphSessionsStub{all: []model.ActiveSession{
		{Session: model.Session{ID: 1, StartAt: start, EndAt: &sessionEnd}, Activity: model.Activity{Name: "Design", ProjectID: 7}},
		{Session: model.Session{ID: 2, StartAt: start, EndAt: &secondEnd}, Activity: model.Activity{Name: "Build", ProjectID: 7}},
	}}
	tags := &graphTagsStub{bySession: map[int64][]model.Tag{1: {{Name: "urgent"}}, 2: {{Name: "later"}}}}
	projects := graphProjectsStub{project: model.Project{ID: 7, Name: "Project", Slug: "project"}}
	builder := &Builder{sessions: reader, projects: projects, tags: tags}
	result, err := builder.BuildStats(context.Background(), StatsQuery{
		TeamID: 4, From: start.Add(-time.Hour), To: end, Now: end,
		ProjectSlug: "project", Tag: "urgent", Uncategorized: "Uncategorized",
	})
	if err != nil {
		t.Fatalf("build stats: %v", err)
	}
	if result.Project.ID != 7 || reader.projectID != 7 {
		t.Fatalf("selected project=%d query project=%d, want 7", result.Project.ID, reader.projectID)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].Session.ID != 1 {
		t.Fatalf("filtered stats sessions = %#v, want only session 1", result.Sessions)
	}
	if result.Summary.TotalSeconds != 60*60 || len(result.Summary.Activities) != 1 || result.Summary.Activities[0].Name != "Design" {
		t.Fatalf("tag filter was not applied before aggregation: %+v", result.Summary)
	}
}
