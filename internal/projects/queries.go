package projects

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

func (s *Service) List(ctx context.Context, query appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	return s.catalog.ListProjects(ctx, query)
}

func (s *Service) activityCounts(ctx context.Context, teamID int64) (map[int64]int, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	counts, err := s.usage.ProjectActivityCounts(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load project activity counts: %w", err)
	}
	return counts, nil
}

// ListWithActivityCounts assembles the project catalog and counts for CLI
// output without making the transport coordinate two workflow reads.
func (s *Service) ListWithActivityCounts(ctx context.Context, query appmodel.ProjectCatalogQuery) (appmodel.ProjectCatalogSnapshot, error) {
	if query.TeamID <= 0 {
		return appmodel.ProjectCatalogSnapshot{}, ErrInvalidTeam
	}
	items, err := s.catalog.ListProjects(ctx, query)
	if err != nil {
		return appmodel.ProjectCatalogSnapshot{}, fmt.Errorf("list projects: %w", err)
	}
	counts, err := s.activityCounts(ctx, query.TeamID)
	if err != nil {
		return appmodel.ProjectCatalogSnapshot{}, fmt.Errorf("load project activity counts: %w", err)
	}
	return appmodel.ProjectCatalogSnapshot{Projects: items, ActivityCounts: counts}, nil
}

// ListWithUsage assembles the project list and its time-window summaries for
// one adapter read, keeping list composition in the project workflow.
func (s *Service) ListWithUsage(ctx context.Context, query appmodel.ProjectUsageQuery) (appmodel.ProjectListSnapshot, error) {
	if query.Catalog.TeamID <= 0 {
		return appmodel.ProjectListSnapshot{}, ErrInvalidTeam
	}
	catalog, err := s.ListWithActivityCounts(ctx, query.Catalog)
	if err != nil {
		return appmodel.ProjectListSnapshot{}, err
	}
	usage, err := s.usageSummaryWithCounts(ctx, query.Catalog.TeamID, query.TodayStart, query.MonthStart, query.Now, catalog.ActivityCounts)
	if err != nil {
		return appmodel.ProjectListSnapshot{}, fmt.Errorf("load project usage summary: %w", err)
	}
	return appmodel.ProjectListSnapshot{Projects: catalog.Projects, Usage: usage}, nil
}

func (s *Service) usageSummaryWithCounts(ctx context.Context, teamID int64, todayStart, monthStart, now time.Time, counts map[int64]int) (map[int64]appmodel.ProjectUsage, error) {
	if teamID <= 0 || todayStart.IsZero() || monthStart.IsZero() || now.IsZero() || todayStart.After(now) || monthStart.After(now) {
		return nil, model.ErrNotFound
	}
	spans, err := s.usage.ProjectSpans(ctx, appmodel.ProjectSpansQuery{TeamID: teamID, From: monthStart, Through: now})
	if err != nil {
		return nil, fmt.Errorf("load project time spans: %w", err)
	}
	usage := make(map[int64]appmodel.ProjectUsage, len(counts))
	for projectID, count := range counts {
		usage[projectID] = appmodel.ProjectUsage{ActivityCount: count}
	}
	for _, span := range spans {
		item := usage[span.ProjectID]
		item.TodaySeconds, err = money.AddInt(item.TodaySeconds, span.Session.TrackedSecondsInWindow(todayStart, now, now))
		if err != nil {
			return nil, fmt.Errorf("sum today's time for project %d: %w", span.ProjectID, err)
		}
		item.MonthSeconds, err = money.AddInt(item.MonthSeconds, span.Session.TrackedSecondsInWindow(monthStart, now, now))
		if err != nil {
			return nil, fmt.Errorf("sum monthly time for project %d: %w", span.ProjectID, err)
		}
		usage[span.ProjectID] = item
	}
	return usage, nil
}

func (s *Service) Summaries(ctx context.Context, query appmodel.ProjectSummariesQuery) (map[int64]appmodel.ProjectSummary, error) {
	if query.TeamID <= 0 {
		return nil, model.ErrNotFound
	}
	for _, id := range query.ProjectIDs {
		if id <= 0 {
			return nil, model.ErrNotFound
		}
	}
	summaries, err := s.usage.ProjectSummaries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load project summaries: %w", err)
	}
	return summaries, nil
}

func (s *Service) Activity(ctx context.Context, query appmodel.ProjectActivityQuery) (ActivitySummary, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 || query.From.IsZero() || query.Through.IsZero() || query.Through.Before(query.From) {
		return ActivitySummary{}, model.ErrNotFound
	}
	recent, err := s.usage.ProjectSessions(ctx, query)
	if err != nil {
		return ActivitySummary{}, fmt.Errorf("load project sessions: %w", err)
	}
	recentSeconds, err := sumRecentProjectTime(recent, query.From, query.Through)
	if err != nil {
		return ActivitySummary{}, err
	}
	total, err := s.usage.ProjectTrackedTotal(ctx, appmodel.ProjectScopeQuery{TeamID: query.TeamID, ProjectID: query.ProjectID})
	if err != nil {
		return ActivitySummary{}, fmt.Errorf("load project tracked total: %w", err)
	}
	return ActivitySummary{Recent: recent, RecentSeconds: recentSeconds, TotalSeconds: total}, nil
}

func sumRecentProjectTime(recent []model.ActiveSession, from, through time.Time) (int, error) {
	total := 0
	for _, item := range recent {
		var err error
		total, err = money.AddInt(total, item.Session.TrackedSecondsInWindow(from, through, through))
		if err != nil {
			return 0, fmt.Errorf("sum recent project time: %w", err)
		}
	}
	return total, nil
}

func (s *Service) Currency(ctx context.Context, query appmodel.ProjectScopeQuery) (string, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return "", model.ErrNotFound
	}
	currency, err := s.billing.ProjectCurrency(ctx, query)
	if err != nil {
		return "", fmt.Errorf("load project currency: %w", err)
	}
	return currency, nil
}

func (s *Service) Currencies(ctx context.Context, teamID int64) (map[int64]string, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	currencies, err := s.billing.ProjectCurrencies(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load project currencies: %w", err)
	}
	return currencies, nil
}

func (s *Service) Activities(ctx context.Context, query appmodel.ProjectActivityCatalogQuery) ([]model.Activity, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return nil, model.ErrNotFound
	}
	return s.catalog.ListActivitiesForProject(ctx, query)
}

func (s *Service) GetInTeam(ctx context.Context, query appmodel.ProjectScopeQuery) (model.Project, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	return s.catalog.GetProjectInTeam(ctx, query)
}

func (s *Service) GetBySlug(ctx context.Context, query appmodel.ProjectSlugQuery) (model.Project, error) {
	query.Slug = strings.TrimSpace(query.Slug)
	if query.TeamID <= 0 || query.Slug == "" {
		return model.Project{}, model.ErrNotFound
	}
	return s.catalog.GetProjectBySlug(ctx, query)
}

// Detail loads the scoped project page data through the owning project workflow.
func (s *Service) Detail(ctx context.Context, request appmodel.ProjectDetailRequest) (appmodel.ProjectDetail, error) {
	if request.TeamID <= 0 || strings.TrimSpace(request.Slug) == "" || request.From.IsZero() || request.Through.IsZero() || request.Through.Before(request.From) {
		return appmodel.ProjectDetail{}, model.ErrNotFound
	}
	project, err := s.catalog.GetProjectBySlug(ctx, appmodel.ProjectSlugQuery{TeamID: request.TeamID, Slug: strings.TrimSpace(request.Slug)})
	if err != nil {
		return appmodel.ProjectDetail{}, err
	}
	activities, err := s.catalog.ListActivitiesForProject(ctx, appmodel.ProjectActivityCatalogQuery{TeamID: request.TeamID, ProjectID: project.ID, IncludeArchived: request.IncludeArchived})
	if err != nil {
		return appmodel.ProjectDetail{}, fmt.Errorf("list project activities: %w", err)
	}
	activity, err := s.Activity(ctx, appmodel.ProjectActivityQuery{TeamID: request.TeamID, ProjectID: project.ID, From: request.From, Through: request.Through})
	if err != nil {
		return appmodel.ProjectDetail{}, fmt.Errorf("load project activity summary: %w", err)
	}
	currency, err := s.Currency(ctx, appmodel.ProjectScopeQuery{TeamID: request.TeamID, ProjectID: project.ID})
	if err != nil {
		return appmodel.ProjectDetail{}, fmt.Errorf("load project currency: %w", err)
	}
	estimatePercent := 0
	if project.EstimateMinutes != nil && *project.EstimateMinutes > 0 {
		estimatePercent = money.PercentRatio(activity.TotalSeconds, *project.EstimateMinutes, 5, 3)
	}
	return appmodel.ProjectDetail{
		Project: project, Activities: activities, Activity: activity,
		Currency: currency, EstimatePercent: estimatePercent,
	}, nil
}
