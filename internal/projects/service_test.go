package projects

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type detailCatalogStub struct {
	ProjectCatalogStore
	project         model.Project
	activities      []model.Activity
	activityQuery   appmodel.ProjectActivityCatalogQuery
	includeArchived bool
}

func (stub *detailCatalogStub) GetProjectBySlug(_ context.Context, query appmodel.ProjectSlugQuery) (model.Project, error) {
	if query.TeamID != stub.project.TeamID || query.Slug != stub.project.Slug {
		return model.Project{}, model.ErrNotFound
	}
	return stub.project, nil
}

func (stub *detailCatalogStub) ListActivitiesForProject(_ context.Context, query appmodel.ProjectActivityCatalogQuery) ([]model.Activity, error) {
	if query.TeamID != stub.project.TeamID || query.ProjectID != stub.project.ID {
		return nil, model.ErrNotFound
	}
	stub.activityQuery, stub.includeArchived = query, query.IncludeArchived
	return stub.activities, nil
}

type detailUsageStub struct {
	ProjectUsageStore
	query  appmodel.ProjectActivityQuery
	scope  appmodel.ProjectScopeQuery
	recent []appmodel.ActiveSession
	total  int
}

func (stub *detailUsageStub) ProjectSessions(_ context.Context, query appmodel.ProjectActivityQuery) ([]appmodel.ActiveSession, error) {
	stub.query = query
	return stub.recent, nil
}

func (stub *detailUsageStub) ProjectTrackedTotal(_ context.Context, query appmodel.ProjectScopeQuery) (int, error) {
	stub.scope = query
	return stub.total, nil
}

type detailBillingStub struct {
	ProjectBillingStore
	currency string
	query    appmodel.ProjectScopeQuery
}

type projectListCatalogStub struct {
	ProjectCatalogStore
	projects        []model.Project
	teamID          int64
	includeArchived bool
}

func (stub *projectListCatalogStub) ListProjects(_ context.Context, query appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	stub.teamID, stub.includeArchived = query.TeamID, query.IncludeArchived
	return stub.projects, nil
}

type projectListUsageStub struct {
	ProjectUsageStore
	counts map[int64]int
	spans  []appmodel.ProjectSessionSpan
}

type projectSummariesUsageStub struct {
	ProjectUsageStore
	query appmodel.ProjectSummariesQuery
}

func (stub *projectSummariesUsageStub) ProjectSummaries(_ context.Context, query appmodel.ProjectSummariesQuery) (map[int64]appmodel.ProjectSummary, error) {
	stub.query = query
	return map[int64]appmodel.ProjectSummary{7: {ID: 7, Name: "Alpha"}}, nil
}

type projectMutationWriterStub struct {
	ProjectWriteStore
	updated appmodel.ProjectUpdateRequest
	deleted appmodel.ProjectMutationRequest
}

func (stub *projectMutationWriterStub) UpdateProjectWithOptions(_ context.Context, request appmodel.ProjectUpdateRequest) (model.Project, error) {
	stub.updated = request
	return model.Project{ID: request.ProjectID, TeamID: request.TeamID}, nil
}

func (stub *projectMutationWriterStub) DeleteProject(_ context.Context, request appmodel.ProjectMutationRequest) error {
	stub.deleted = request
	return nil
}

func (stub projectListUsageStub) ProjectActivityCounts(context.Context, int64) (map[int64]int, error) {
	return stub.counts, nil
}

func (stub projectListUsageStub) ProjectSpans(context.Context, appmodel.ProjectSpansQuery) ([]appmodel.ProjectSessionSpan, error) {
	return stub.spans, nil
}

func (stub *detailBillingStub) ProjectCurrency(_ context.Context, query appmodel.ProjectScopeQuery) (string, error) {
	stub.query = query
	return stub.currency, nil
}

func TestListRejectsUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.List(context.Background(), appmodel.ProjectCatalogQuery{}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("List with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

func TestGetBySlugTrimsSlugWithinWorkspaceQuery(t *testing.T) {
	project := model.Project{ID: 7, TeamID: 3, Slug: "alpha"}
	service := &Service{catalog: &detailCatalogStub{project: project}}
	got, err := service.GetBySlug(context.Background(), appmodel.ProjectSlugQuery{TeamID: 3, Slug: " alpha "})
	if err != nil || got != project {
		t.Fatalf("GetBySlug = %+v, %v", got, err)
	}
}

func TestListWithUsageAssemblesScopedProjectSnapshot(t *testing.T) {
	start := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	now := start.Add(3 * time.Hour)
	monthStart := start.AddDate(0, 0, -30)
	ended := start.Add(2 * time.Hour)
	project := model.Project{ID: 7, TeamID: 3, Slug: "alpha", Name: "Alpha", Archived: true}
	catalog := &projectListCatalogStub{projects: []model.Project{project}}
	service := &Service{
		catalog: catalog,
		usage: projectListUsageStub{
			counts: map[int64]int{7: 2},
			spans: []appmodel.ProjectSessionSpan{{ProjectID: 7, Session: model.Session{
				StartAt: start, EndAt: &ended, AccumulatedSeconds: 2 * 60 * 60,
			}}},
		},
	}
	catalogSnapshot, err := service.ListWithActivityCounts(context.Background(), appmodel.ProjectCatalogQuery{TeamID: 3, IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListWithActivityCounts: %v", err)
	}
	if len(catalogSnapshot.Projects) != 1 || catalogSnapshot.Projects[0] != project || catalogSnapshot.ActivityCounts[7] != 2 {
		t.Fatalf("catalog snapshot = %+v, want project and activity count", catalogSnapshot)
	}
	if catalog.teamID != 3 || !catalog.includeArchived {
		t.Fatalf("catalog request scope = team %d archived %v, want team 3 archived true", catalog.teamID, catalog.includeArchived)
	}

	snapshot, err := service.ListWithUsage(context.Background(), appmodel.ProjectUsageQuery{
		Catalog:    appmodel.ProjectCatalogQuery{TeamID: 3, IncludeArchived: true},
		TodayStart: start, MonthStart: monthStart, Now: now,
	})
	if err != nil {
		t.Fatalf("ListWithUsage: %v", err)
	}
	if !catalog.includeArchived || len(snapshot.Projects) != 1 || snapshot.Projects[0] != project {
		t.Fatalf("project snapshot = %+v, archived = %v", snapshot, catalog.includeArchived)
	}
	if got := snapshot.Usage[7]; got.ActivityCount != 2 || got.TodaySeconds != 7200 || got.MonthSeconds != 7200 {
		t.Fatalf("project usage = %+v, want count 2 and 7200 seconds", got)
	}
}

func TestSummariesForwardsWorkspaceAndProjectIDs(t *testing.T) {
	store := &projectSummariesUsageStub{}
	service := &Service{usage: store}
	query := appmodel.ProjectSummariesQuery{TeamID: 3, ProjectIDs: []int64{7, 8}}
	got, err := service.Summaries(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if got[7].Name != "Alpha" || store.query.TeamID != query.TeamID || !reflect.DeepEqual(store.query.ProjectIDs, query.ProjectIDs) {
		t.Fatalf("summary query/result = %+v / %+v", store.query, got)
	}
}

func TestSlugMutationsResolveProjectInsideWorkflow(t *testing.T) {
	catalog := &detailCatalogStub{project: model.Project{ID: 17, TeamID: 3, Slug: "alpha"}}
	writes := &projectMutationWriterStub{}
	service := &Service{catalog: catalog, writes: writes}

	updated, err := service.UpdateBySlug(context.Background(), appmodel.ProjectSlugUpdateRequest{
		TeamID: 3, Slug: " alpha ", CallerID: 12, Update: appmodel.ProjectUpdate{Name: "Renamed"},
	})
	if err != nil {
		t.Fatalf("UpdateBySlug: %v", err)
	}
	if updated.ID != 17 || writes.updated.TeamID != 3 || writes.updated.ProjectID != 17 || writes.updated.CallerID != 12 || writes.updated.Update.Name != "Renamed" {
		t.Fatalf("resolved update = %+v, result = %+v", writes.updated, updated)
	}

	if err := service.DeleteBySlug(context.Background(), appmodel.ProjectSlugMutationRequest{
		TeamID: 3, Slug: "alpha", CallerID: 12,
	}); err != nil {
		t.Fatalf("DeleteBySlug: %v", err)
	}
	if writes.deleted.TeamID != 3 || writes.deleted.ProjectID != 17 || writes.deleted.CallerID != 12 {
		t.Fatalf("resolved delete = %+v", writes.deleted)
	}
}

func TestSlugMutationsDoNotWriteForUnknownProject(t *testing.T) {
	catalog := &detailCatalogStub{project: model.Project{ID: 17, TeamID: 3, Slug: "alpha"}}
	writes := &projectMutationWriterStub{}
	service := &Service{catalog: catalog, writes: writes}

	if _, err := service.UpdateBySlug(context.Background(), appmodel.ProjectSlugUpdateRequest{
		TeamID: 4, Slug: "alpha", CallerID: 12,
	}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("UpdateBySlug unknown project error = %v", err)
	}
	if err := service.DeleteBySlug(context.Background(), appmodel.ProjectSlugMutationRequest{
		TeamID: 4, Slug: "alpha", CallerID: 12,
	}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("DeleteBySlug unknown project error = %v", err)
	}
	if writes.updated.ProjectID != 0 || writes.deleted.ProjectID != 0 {
		t.Fatalf("unknown project reached writes: update=%+v delete=%+v", writes.updated, writes.deleted)
	}
}

func TestSumRecentProjectTimeUsesRequestedWindowAndExcludesPause(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	pausedAt := from.Add(time.Hour)
	recent := []appmodel.ActiveSession{{Session: model.Session{
		StartAt: from, Paused: true, PausedAt: &pausedAt,
		AccumulatedSeconds: 3600,
	}}}

	got, err := sumRecentProjectTime(recent, from, through)
	if err != nil {
		t.Fatalf("sumRecentProjectTime() error = %v", err)
	}
	if got != 3600 {
		t.Fatalf("sumRecentProjectTime() = %d, want 3600", got)
	}
}

func TestDetailLoadsScopedProjectPageData(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	estimateMinutes := 120
	project := model.Project{ID: 7, TeamID: 3, Slug: "alpha", Name: "Alpha", EstimateMinutes: &estimateMinutes}
	activity := model.Activity{ID: 11, TeamID: 3, ProjectID: 7, Name: "Build"}
	started := from.Add(time.Hour)
	ended := started.Add(30 * time.Minute)
	catalog := &detailCatalogStub{project: project, activities: []model.Activity{activity}}
	usage := &detailUsageStub{
		recent: []appmodel.ActiveSession{{Session: model.Session{StartAt: started, EndAt: &ended, AccumulatedSeconds: 1800}, Activity: activity}},
		total:  7200,
	}
	billing := &detailBillingStub{currency: "USD"}
	service := &Service{
		catalog: catalog, usage: usage, billing: billing,
	}

	got, err := service.Detail(context.Background(), appmodel.ProjectDetailRequest{
		TeamID: 3, Slug: "alpha", IncludeArchived: true, From: from, Through: through,
	})
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if got.Project != project || len(got.Activities) != 1 || got.Activities[0] != activity {
		t.Fatalf("Detail() project/activity = %#v / %#v", got.Project, got.Activities)
	}
	if !catalog.includeArchived || catalog.activityQuery.TeamID != 3 || catalog.activityQuery.ProjectID != 7 {
		t.Fatal("Detail() did not pass through the archived-activity option")
	}
	if got.Activity.RecentSeconds != 1800 || got.Activity.TotalSeconds != 7200 || got.Currency != "USD" || got.EstimatePercent != 100 {
		t.Fatalf("Detail() summary = %#v, currency %q", got.Activity, got.Currency)
	}
	wantQuery := appmodel.ProjectActivityQuery{TeamID: 3, ProjectID: 7, From: from, Through: through}
	wantScope := appmodel.ProjectScopeQuery{TeamID: 3, ProjectID: 7}
	if usage.query != wantQuery || usage.scope != wantScope || billing.query != wantScope {
		t.Fatalf("detail read scopes = activity %+v, total %+v, currency %+v", usage.query, usage.scope, billing.query)
	}
}
