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

func TestBuildGraphSessionsAppliesProjectAndTagFilters(t *testing.T) {
	reader := &graphSessionsStub{all: []model.ActiveSession{
		{Session: model.Session{ID: 1}, Activity: model.Activity{ProjectID: 7}},
		{Session: model.Session{ID: 2}, Activity: model.Activity{ProjectID: 7}},
	}}
	tags := &graphTagsStub{bySession: map[int64][]model.Tag{1: {{Name: "urgent"}}, 2: {{Name: "later"}}}}
	builder := &Builder{sessions: reader, tags: tags, projects: graphProjectsStub{project: model.Project{ID: 7, Name: "Project", Slug: "project"}}}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := builder.BuildGraphSessions(context.Background(), GraphQuery{
		TeamID: 4, From: from, To: from.AddDate(0, 0, 1), ProjectSlug: "project", Tag: "urgent",
	})
	if err != nil {
		t.Fatalf("build graph sessions: %v", err)
	}
	if reader.projectID != 7 {
		t.Fatalf("project query id = %d, want 7", reader.projectID)
	}
	if tags.calls != 1 {
		t.Fatalf("tag lookup calls = %d, want 1", tags.calls)
	}
	if got.Project.ID != 7 || len(got.Sessions) != 1 || got.Sessions[0].Session.ID != 1 {
		t.Fatalf("filtered graph result = %#v, want project 7 and only session 1", got)
	}
}

func TestBuildGraphSessionsSkipsTagReadWithoutTagFilter(t *testing.T) {
	reader := &graphSessionsStub{all: []model.ActiveSession{{Session: model.Session{ID: 1}}}}
	tags := &graphTagsStub{}
	builder := &Builder{sessions: reader, tags: tags}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := builder.BuildGraphSessions(context.Background(), GraphQuery{TeamID: 4, From: from, To: from.Add(time.Hour)})
	if err != nil {
		t.Fatalf("build graph sessions: %v", err)
	}
	if len(got.Sessions) != 1 || tags.calls != 0 {
		t.Fatalf("sessions=%d tag reads=%d; want 1 session and no tag read", len(got.Sessions), tags.calls)
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
