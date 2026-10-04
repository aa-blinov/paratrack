package reports

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/sessiondecorations"
)

type graphSessionsStub struct {
	all       []model.ActiveSession
	projectID int64
	teamID    int64
	from      time.Time
	to        time.Time
	scopedID  int64
}

func (s *graphSessionsStub) ClosedSessions(ctx context.Context, query appmodel.ClosedSessionsQuery) ([]model.ActiveSession, error) {
	s.teamID, s.from, s.to = query.TeamID, query.Start, query.End
	if query.ProjectID != nil {
		s.projectID = *query.ProjectID
	}
	s.scopedID = requestctx.ScopedUserID(ctx)
	return s.all, nil
}

type reportTeamsStub struct{ members []model.TeamMember }

type sessionReaderStub struct{}

func (sessionReaderStub) SessionActivity(context.Context, appmodel.SessionLookupQuery) (model.Session, model.Activity, error) {
	return model.Session{}, model.Activity{}, nil
}

func (s reportTeamsStub) Members(context.Context, int64) ([]model.TeamMember, error) {
	return s.members, nil
}
func (reportTeamsStub) Currency(context.Context, int64) (string, error) { return "RUB", nil }

type graphTagsStub struct {
	calls     int
	bySession map[int64][]model.Tag
}

func (*graphTagsStub) List(context.Context, int64) ([]model.Tag, error) { return nil, nil }
func (s *graphTagsStub) TagsForSessions(context.Context, appmodel.SessionTagsQuery) (map[int64][]model.Tag, error) {
	s.calls++
	return s.bySession, nil
}

type graphProjectsStub struct {
	project    model.Project
	summaryErr error
}

func (s graphProjectsStub) List(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	return []model.Project{s.project}, nil
}
func (s graphProjectsStub) GetBySlug(context.Context, appmodel.ProjectSlugQuery) (model.Project, error) {
	return s.project, nil
}
func (graphProjectsStub) Currencies(context.Context, int64) (map[int64]string, error) {
	return nil, nil
}
func (s graphProjectsStub) Summaries(_ context.Context, query appmodel.ProjectSummariesQuery) (map[int64]model.ProjectSummary, error) {
	if s.summaryErr != nil {
		return nil, s.summaryErr
	}
	result := make(map[int64]model.ProjectSummary)
	for _, id := range query.ProjectIDs {
		if id == s.project.ID {
			result[id] = model.ProjectSummary{ID: id, Name: s.project.Name}
		}
	}
	return result, nil
}

type reportLoggerStub struct{ messages []string }

func (stub *reportLoggerStub) Printf(format string, args ...any) {
	stub.messages = append(stub.messages, fmt.Sprintf(format, args...))
}

func decorationsForTest(t *testing.T, projects ProjectReader, tags TagReader, logger *reportLoggerStub) SessionDecorationBuilder {
	t.Helper()
	builder, err := sessiondecorations.New(sessiondecorations.Dependencies{Sessions: sessionReaderStub{}, Tags: tags, Projects: projects, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return builder
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

func TestBuildGraphScopesOnlyToNamedMemberOfTheWorkspace(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	reader := &graphSessionsStub{}
	builder := &Builder{
		sessions: reader, teams: reportTeamsStub{members: []model.TeamMember{
			{UserID: 5, Email: "five@example.test"}, {UserID: 6},
		}},
	}
	result, err := builder.BuildGraph(context.Background(), GraphQuery{TeamID: 4, From: from, To: to, Now: to, PersonID: 5})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if result.PersonFilter != 5 || result.PersonName != "five@example.test" || reader.scopedID != 5 {
		t.Fatalf("member report scope = result(%d, %q), query scope %d", result.PersonFilter, result.PersonName, reader.scopedID)
	}
	reader.scopedID = 0
	result, err = builder.BuildGraph(context.Background(), GraphQuery{TeamID: 4, From: from, To: to, Now: to, PersonID: 6})
	if err != nil {
		t.Fatalf("build graph for unnamed member: %v", err)
	}
	if result.PersonFilter != 0 || reader.scopedID != 0 {
		t.Fatalf("unnamed member received a report scope: result=%d query=%d", result.PersonFilter, reader.scopedID)
	}
}

func TestBuildStatsReturnsMemberOptionsAndSelectedScope(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	reader := &graphSessionsStub{}
	projects := graphProjectsStub{}
	tags := &graphTagsStub{}
	builder := &Builder{
		sessions: reader, projects: projects, teams: reportTeamsStub{members: []model.TeamMember{
			{UserID: 5, Email: "five@example.test"}, {UserID: 6, Name: "Six"}, {UserID: 7},
		}}, tags: tags, decorations: decorationsForTest(t, projects, tags, &reportLoggerStub{}),
	}
	result, err := builder.BuildStats(context.Background(), StatsQuery{
		TeamID: 4, From: from, To: to, Now: to, PersonID: 6, IncludePeople: true, Uncategorized: "Uncategorized",
	})
	if err != nil {
		t.Fatalf("build stats: %v", err)
	}
	if result.PersonFilter != 6 || reader.scopedID != 6 || len(result.People) != 2 ||
		result.People[0].Name != "five@example.test" || !result.People[1].Selected {
		t.Fatalf("stats member selection = filter %d, scope %d, options %+v", result.PersonFilter, reader.scopedID, result.People)
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
	builder := &Builder{sessions: reader, projects: projects, tags: tags, decorations: decorationsForTest(t, projects, tags, &reportLoggerStub{})}
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
	if result.ProjectsByID[7].Name != "Project" {
		t.Fatalf("stats session project summaries = %#v, want project 7", result.ProjectsByID)
	}
	if result.Summary.TotalSeconds != 60*60 || len(result.Summary.Activities) != 1 || result.Summary.Activities[0].Name != "Design" {
		t.Fatalf("tag filter was not applied before aggregation: %+v", result.Summary)
	}
}

func TestBuildStatsKeepsReportAvailableWhenProjectDecorationFails(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	reader := &graphSessionsStub{all: []model.ActiveSession{{
		Session:  model.Session{ID: 1, StartAt: from, EndAt: &to, AccumulatedSeconds: 3600},
		Activity: model.Activity{Name: "Focus", ProjectID: 7},
	}}}
	logger := &reportLoggerStub{}
	projects := graphProjectsStub{project: model.Project{ID: 7}, summaryErr: errors.New("summary store unavailable")}
	tags := &graphTagsStub{}
	builder := &Builder{
		sessions: reader, projects: projects, tags: tags,
		decorations: decorationsForTest(t, projects, tags, logger),
	}
	result, err := builder.BuildStats(context.Background(), StatsQuery{
		TeamID: 4, From: from, To: to, Now: to, Uncategorized: "Uncategorized",
	})
	if err != nil {
		t.Fatalf("build stats with optional project decoration failure: %v", err)
	}
	if result.Summary.TotalSeconds != 3600 || len(result.ProjectsByID) != 0 || len(logger.messages) != 1 {
		t.Fatalf("stats fallback = %+v, logs = %v", result, logger.messages)
	}
}

func TestBuildExportFiltersAndBatchesProjectNames(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	end := from.Add(time.Hour)
	note := "customer work"
	reader := &graphSessionsStub{all: []model.ActiveSession{
		{Session: model.Session{ID: 1, StartAt: from, EndAt: &end, Note: &note}, Activity: model.Activity{Name: "Design", ProjectID: 7}},
		{Session: model.Session{ID: 2, StartAt: from.Add(time.Hour), EndAt: &end}, Activity: model.Activity{Name: "Admin", ProjectID: 0}},
		{Session: model.Session{ID: 3, StartAt: from.AddDate(0, 0, 2), EndAt: &end}, Activity: model.Activity{Name: "Outside", ProjectID: 7}},
	}}
	project := model.Project{ID: 7, Name: "Website"}
	builder := &Builder{sessions: reader, projects: graphProjectsStub{project: project}}
	snapshot, err := builder.BuildExport(context.Background(), appmodel.ExportBuildQuery{
		TeamID: 9, Start: from, End: to, Now: to, HasFrom: true, HasTo: true,
	})
	if err != nil {
		t.Fatalf("build CSV export: %v", err)
	}
	if reader.teamID != 9 || !reader.from.Equal(from) || !reader.to.Equal(to) {
		t.Fatalf("session query team=%d from=%s to=%s", reader.teamID, reader.from, reader.to)
	}
	if len(snapshot.Rows) != 2 {
		t.Fatalf("export row count = %d, want 2: %+v", len(snapshot.Rows), snapshot.Rows)
	}
	if snapshot.Rows[0].ProjectName != "Website" || snapshot.Rows[0].ActivityName != "Design" || snapshot.Rows[0].DurationSeconds != 3600 || snapshot.Rows[0].Note != note {
		t.Fatalf("first export row = %+v", snapshot.Rows[0])
	}
	if snapshot.Rows[1].ProjectName != "" || snapshot.Rows[1].ActivityName != "Admin" {
		t.Fatalf("uncategorized export row = %+v", snapshot.Rows[1])
	}
}
