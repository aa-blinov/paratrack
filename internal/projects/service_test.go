package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type detailCatalogStub struct {
	ProjectCatalogStore
	project         model.Project
	activities      []model.Activity
	includeArchived bool
}

func (stub *detailCatalogStub) GetProjectBySlug(_ context.Context, teamID int64, slug string) (model.Project, error) {
	if teamID != stub.project.TeamID || slug != stub.project.Slug {
		return model.Project{}, model.ErrNotFound
	}
	return stub.project, nil
}

func (stub *detailCatalogStub) ListActivitiesForProject(_ context.Context, teamID, projectID int64, includeArchived bool) ([]model.Activity, error) {
	if teamID != stub.project.TeamID || projectID != stub.project.ID {
		return nil, model.ErrNotFound
	}
	stub.includeArchived = includeArchived
	return stub.activities, nil
}

type detailUsageStub struct {
	ProjectUsageStore
	recent []model.ActiveSession
	total  int
}

func (stub detailUsageStub) ProjectSessions(context.Context, int64, int64, time.Time, time.Time) ([]model.ActiveSession, error) {
	return stub.recent, nil
}

func (stub detailUsageStub) ProjectTrackedTotal(context.Context, int64, int64) (int, error) {
	return stub.total, nil
}

type detailBillingStub struct {
	ProjectBillingStore
	currency string
}

func (stub detailBillingStub) ProjectCurrency(context.Context, int64, int64) (string, error) {
	return stub.currency, nil
}

func TestListRejectsUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.List(context.Background(), 0, false); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("List with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

func TestSumRecentProjectTimeUsesRequestedWindowAndExcludesPause(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	pausedAt := from.Add(time.Hour)
	recent := []model.ActiveSession{{Session: model.Session{
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
	project := model.Project{ID: 7, TeamID: 3, Slug: "alpha", Name: "Alpha"}
	activity := model.Activity{ID: 11, TeamID: 3, ProjectID: 7, Name: "Build"}
	started := from.Add(time.Hour)
	ended := started.Add(30 * time.Minute)
	catalog := &detailCatalogStub{project: project, activities: []model.Activity{activity}}
	service := &Service{
		catalog: catalog,
		usage: detailUsageStub{
			recent: []model.ActiveSession{{Session: model.Session{StartAt: started, EndAt: &ended, AccumulatedSeconds: 1800}, Activity: activity}},
			total:  7200,
		},
		billing: detailBillingStub{currency: "USD"},
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
	if !catalog.includeArchived {
		t.Fatal("Detail() did not pass through the archived-activity option")
	}
	if got.Activity.RecentSeconds != 1800 || got.Activity.TotalSeconds != 7200 || got.Currency != "USD" {
		t.Fatalf("Detail() summary = %#v, currency %q", got.Activity, got.Currency)
	}
}
