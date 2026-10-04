// Package projects owns project workflows shared by HTTP and other adapters.
package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ProjectCatalogStore provides project and activity lookups.
type ProjectCatalogStore interface {
	ListProjects(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error)
	ListActivitiesForProject(context.Context, appmodel.ProjectActivityCatalogQuery) ([]model.Activity, error)
	GetProjectInTeam(context.Context, appmodel.ProjectScopeQuery) (model.Project, error)
	GetProjectBySlug(context.Context, appmodel.ProjectSlugQuery) (model.Project, error)
	GetActivity(context.Context, appmodel.ActivityLookupQuery) (model.Activity, error)
}

// ProjectUsageStore provides team-scoped usage summaries.
type ProjectUsageStore interface {
	ProjectActivityCounts(context.Context, int64) (map[int64]int, error)
	ProjectSpans(context.Context, appmodel.ProjectSpansQuery) ([]appmodel.ProjectSessionSpan, error)
	ProjectSummaries(context.Context, appmodel.ProjectSummariesQuery) (map[int64]appmodel.ProjectSummary, error)
	ProjectSessions(context.Context, appmodel.ProjectActivityQuery) ([]appmodel.ActiveSession, error)
	ProjectTrackedTotal(context.Context, appmodel.ProjectScopeQuery) (int, error)
}

// ProjectBillingStore provides billing defaults and project currencies.
type ProjectBillingStore interface {
	ProjectCurrency(context.Context, appmodel.ProjectScopeQuery) (string, error)
	ProjectCurrencies(context.Context, int64) (map[int64]string, error)
}

// ProjectAuthorizationReader resolves the caller's current workspace role.
// The write ports still repeat authorization checks transactionally.
type ProjectAuthorizationReader interface {
	TeamMemberRole(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error)
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

type ActivitySummary = appmodel.ProjectActivitySummary

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
