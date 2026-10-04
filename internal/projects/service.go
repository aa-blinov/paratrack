// Package projects owns project workflows shared by HTTP and other adapters.
package projects

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/translit"
)

// ProjectCatalogStore provides project and activity lookups.
type ProjectCatalogStore interface {
	ListProjects(context.Context, int64, bool) ([]model.Project, error)
	ListActivitiesForProject(context.Context, int64, int64, bool) ([]model.Activity, error)
	GetProjectInTeam(context.Context, int64, int64) (model.Project, error)
	GetProjectBySlug(context.Context, int64, string) (model.Project, error)
	GetActivity(context.Context, int64, int64) (model.Activity, error)
}

// ProjectUsageStore provides team-scoped usage summaries.
type ProjectUsageStore interface {
	ProjectActivityCounts(context.Context, int64) (map[int64]int, error)
	ProjectSpans(context.Context, int64, time.Time, time.Time) ([]model.ProjectSessionSpan, error)
	ProjectSummaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error)
	ProjectSessions(context.Context, int64, int64, time.Time, time.Time) ([]model.ActiveSession, error)
	ProjectTrackedTotal(context.Context, int64, int64) (int, error)
}

// ProjectBillingStore provides billing defaults and project currencies.
type ProjectBillingStore interface {
	ProjectCurrency(context.Context, int64, int64) (string, error)
	ProjectCurrencies(context.Context, int64) (map[int64]string, error)
}

// ProjectAuthorizationReader resolves the caller's current workspace role.
// The write ports still repeat authorization checks transactionally.
type ProjectAuthorizationReader interface {
	TeamMemberRole(context.Context, int64, int64) (model.TeamRole, bool, error)
}

// ProjectWriteStore provides atomic project and activity mutations.
type ProjectWriteStore interface {
	CreateProjectWithBilling(context.Context, appmodel.ProjectCreateRequest) (model.Project, error)
	UpdateProjectWithOptions(context.Context, appmodel.ProjectUpdateRequest) (model.Project, error)
	DeleteProject(context.Context, appmodel.ProjectMutationRequest) error
	AssignActivityProject(context.Context, appmodel.AssignActivityProjectRequest) error
	AssignFirstActivityProject(context.Context, appmodel.AssignActivityProjectRequest) error
	SetProjectRate(context.Context, appmodel.ProjectRateRequest) error
}

// Dependencies keeps catalog, usage, billing and write workflows on
// independently substitutable persistence ports.
type Dependencies struct {
	Catalog       ProjectCatalogStore
	Usage         ProjectUsageStore
	Billing       ProjectBillingStore
	Authorization ProjectAuthorizationReader
	Writes        ProjectWriteStore
}

var ErrIncompleteDependencies = errors.New("project service dependencies are incomplete")

type Service struct {
	catalog       ProjectCatalogStore
	usage         ProjectUsageStore
	billing       ProjectBillingStore
	authorization ProjectAuthorizationReader
	writes        ProjectWriteStore
}

type ActivitySummary = model.ProjectActivitySummary

func NewService(deps Dependencies) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"catalog", deps.Catalog}, {"usage", deps.Usage},
		{"billing", deps.Billing}, {"authorization", deps.Authorization}, {"writes", deps.Writes},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{
		catalog: deps.Catalog, usage: deps.Usage,
		billing: deps.Billing, authorization: deps.Authorization, writes: deps.Writes,
	}, nil
}

func (s *Service) List(ctx context.Context, teamID int64, includeArchived bool) ([]model.Project, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	return s.catalog.ListProjects(ctx, teamID, includeArchived)
}

// ActivityCounts returns the activity count per project in one workspace read.
func (s *Service) ActivityCounts(ctx context.Context, teamID int64) (map[int64]int, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	counts, err := s.usage.ProjectActivityCounts(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load project activity counts: %w", err)
	}
	return counts, nil
}

// UsageSummary returns activity counts and tracked time for the projects in
// the requested workspace. todayStart is local midnight; monthStart may be a
// rolling window boundary chosen by the caller.
func (s *Service) UsageSummary(ctx context.Context, teamID int64, todayStart, monthStart, now time.Time) (map[int64]model.ProjectUsage, error) {
	if teamID <= 0 || todayStart.IsZero() || monthStart.IsZero() || now.IsZero() || todayStart.After(now) || monthStart.After(now) {
		return nil, model.ErrNotFound
	}
	counts, err := s.ActivityCounts(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load project activity counts: %w", err)
	}
	spans, err := s.usage.ProjectSpans(ctx, teamID, monthStart, now)
	if err != nil {
		return nil, fmt.Errorf("load project time spans: %w", err)
	}
	usage := make(map[int64]model.ProjectUsage, len(counts))
	for projectID, count := range counts {
		usage[projectID] = model.ProjectUsage{ActivityCount: count}
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

func (s *Service) Summaries(ctx context.Context, teamID int64, projectIDs []int64) (map[int64]model.ProjectSummary, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	for _, id := range projectIDs {
		if id <= 0 {
			return nil, model.ErrNotFound
		}
	}
	summaries, err := s.usage.ProjectSummaries(ctx, teamID, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("load project summaries: %w", err)
	}
	return summaries, nil
}

func (s *Service) Activity(ctx context.Context, teamID, projectID int64, from, through time.Time) (ActivitySummary, error) {
	if teamID <= 0 || projectID <= 0 || from.IsZero() || through.IsZero() || through.Before(from) {
		return ActivitySummary{}, model.ErrNotFound
	}
	recent, err := s.usage.ProjectSessions(ctx, teamID, projectID, from, through)
	if err != nil {
		return ActivitySummary{}, fmt.Errorf("load project sessions: %w", err)
	}
	recentSeconds, err := sumRecentProjectTime(recent, from, through)
	if err != nil {
		return ActivitySummary{}, err
	}
	total, err := s.usage.ProjectTrackedTotal(ctx, teamID, projectID)
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

func (s *Service) Currency(ctx context.Context, teamID, projectID int64) (string, error) {
	if teamID <= 0 || projectID <= 0 {
		return "", model.ErrNotFound
	}
	currency, err := s.billing.ProjectCurrency(ctx, teamID, projectID)
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

func (s *Service) Activities(ctx context.Context, teamID, projectID int64, includeArchived bool) ([]model.Activity, error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, model.ErrNotFound
	}
	return s.catalog.ListActivitiesForProject(ctx, teamID, projectID, includeArchived)
}

func (s *Service) GetInTeam(ctx context.Context, teamID, projectID int64) (model.Project, error) {
	if teamID <= 0 || projectID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	return s.catalog.GetProjectInTeam(ctx, teamID, projectID)
}

func (s *Service) GetBySlug(ctx context.Context, teamID int64, slug string) (model.Project, error) {
	if teamID <= 0 || strings.TrimSpace(slug) == "" {
		return model.Project{}, model.ErrNotFound
	}
	return s.catalog.GetProjectBySlug(ctx, teamID, strings.TrimSpace(slug))
}

var ErrRebindForbidden = model.ErrProjectRebindForbidden

// AssignActivity enforces member and workspace rules before the scoped
// persistence operation attaches an activity to a project.
func (s *Service) AssignActivity(ctx context.Context, request appmodel.AssignActivityProjectRequest) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	if request.ActivityID <= 0 || request.ProjectID < 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	role, member, err := s.authorization.TeamMemberRole(ctx, request.TeamID, request.CallerID)
	if err != nil {
		return fmt.Errorf("resolve project assignment role: %w", err)
	}
	if !member {
		return model.ErrForbidden
	}
	if role.CanManage() {
		return s.writes.AssignActivityProject(ctx, request)
	}
	activity, err := s.catalog.GetActivity(ctx, request.TeamID, request.ActivityID)
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load activity before assigning project: %w", err)
	}
	if activity.ProjectID == request.ProjectID {
		return nil
	}
	if activity.ProjectID != 0 || request.ProjectID == 0 {
		return ErrRebindForbidden
	}
	if err := s.writes.AssignFirstActivityProject(ctx, request); err != nil {
		if errors.Is(err, model.ErrForbidden) {
			return ErrRebindForbidden
		}
		return err
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.ProjectMutationRequest) error {
	if request.TeamID <= 0 {
		return fmt.Errorf("invalid project delete request")
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return fmt.Errorf("invalid project delete request")
	}
	return s.writes.DeleteProject(ctx, request)
}

var (
	ErrDuplicate       = model.ErrAlreadyExists
	ErrNotFound        = model.ErrNotFound
	ErrAmbiguousSlug   = model.ErrAmbiguousProject
	ErrAlreadyBilled   = model.ErrAlreadyBilled
	ErrInvalidTeam     = appmodel.ErrInvalidProjectTeam
	ErrInvalidRate     = appmodel.ErrInvalidProjectRate
	ErrInvalidCurrency = appmodel.ErrInvalidProjectCurrency
	ErrInvalidEstimate = appmodel.ErrInvalidProjectEstimate
	ErrInvalidName     = appmodel.ErrInvalidProjectName
	ErrInvalidSlug     = appmodel.ErrInvalidProjectSlug
	ErrInvalidColor    = appmodel.ErrInvalidProjectColor
)

var projectSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var projectColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeProjectInput(name, slug, color string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", ErrInvalidName
	}
	if strings.TrimSpace(slug) == "" {
		slug = slugify(name)
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !projectSlugPattern.MatchString(slug) {
		return "", "", "", ErrInvalidSlug
	}
	if color == "" {
		color = "#7c8499"
	}
	if !projectColorPattern.MatchString(color) {
		return "", "", "", ErrInvalidColor
	}
	return name, slug, color, nil
}

func slugify(name string) string {
	var b strings.Builder
	previousDash := false
	for _, r := range translit.Latin(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			previousDash = false
		} else if !previousDash && b.Len() > 0 {
			b.WriteByte('-')
			previousDash = true
		}
	}
	if slug := strings.TrimRight(b.String(), "-"); slug != "" {
		return slug
	}
	return "project"
}

// Create validates billing defaults and persists the project atomically.
func (s *Service) Create(ctx context.Context, request appmodel.ProjectCreateRequest) (model.Project, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	var err error
	request.Name, request.Slug, request.Color, err = normalizeProjectInput(request.Name, request.Slug, request.Color)
	if err != nil {
		return model.Project{}, err
	}
	if request.RateCents != nil && *request.RateCents < 0 {
		return model.Project{}, ErrInvalidRate
	}
	request.Currency = strings.TrimSpace(request.Currency)
	if request.Currency != "" && (len(request.Currency) != 3 || strings.ToUpper(request.Currency) != request.Currency) {
		return model.Project{}, ErrInvalidCurrency
	}
	project, err := s.writes.CreateProjectWithBilling(ctx, request)
	if err != nil {
		if errors.Is(err, model.ErrAlreadyExists) {
			return model.Project{}, ErrDuplicate
		}
		return model.Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

// Update applies a project edit, including optional billing settings, as one
// persistence operation.
func (s *Service) Update(ctx context.Context, request appmodel.ProjectUpdateRequest) (model.Project, error) {
	if request.TeamID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	if request.Update.Name != "" {
		request.Update.Name = strings.TrimSpace(request.Update.Name)
		if request.Update.Name == "" {
			return model.Project{}, ErrInvalidName
		}
	}
	if request.Update.Color != "" && !projectColorPattern.MatchString(request.Update.Color) {
		return model.Project{}, ErrInvalidColor
	}
	if request.Update.RateCents != nil && *request.Update.RateCents < 0 {
		return model.Project{}, ErrInvalidRate
	}
	if request.Update.EstimateMinutes != nil && *request.Update.EstimateMinutes < 0 {
		return model.Project{}, ErrInvalidEstimate
	}
	if request.Update.Currency != nil {
		currency := strings.TrimSpace(*request.Update.Currency)
		if currency != "" && (len(currency) != 3 || strings.ToUpper(currency) != currency) {
			return model.Project{}, ErrInvalidCurrency
		}
		request.Update.Currency = &currency
	}
	project, err := s.writes.UpdateProjectWithOptions(ctx, request)
	if err != nil {
		return model.Project{}, fmt.Errorf("update project: %w", err)
	}
	return project, nil
}

// UpdateRate changes the billable rate and flag together while preserving any
// field omitted by the adapter.
func (s *Service) UpdateRate(ctx context.Context, request appmodel.ProjectRateRequest) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if request.RateCents != nil && *request.RateCents < 0 {
		return ErrInvalidRate
	}
	if request.RateCents == nil && request.Billable == nil {
		return nil
	}
	if err := s.writes.SetProjectRate(ctx, request); err != nil {
		return fmt.Errorf("update project rate: %w", err)
	}
	return nil
}
