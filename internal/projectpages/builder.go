// Package projectpages assembles the application read model for project
// detail pages from project, workspace, and invoicing workflows.
package projectpages

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type ProjectReader interface {
	Detail(context.Context, appmodel.ProjectDetailRequest) (model.ProjectDetail, error)
}

type SessionDecorationBuilder interface {
	Build(context.Context, appmodel.SessionDecorationRequest) (appmodel.SessionDecorationSnapshot, error)
}

type TeamSettingsReader interface {
	Currency(context.Context, int64) (string, error)
}

type TeamMembershipReader interface {
	IsMember(context.Context, int64, int64) (model.TeamRole, bool, error)
}

type InvoiceHistoryReader interface {
	UnbilledProjectTime(context.Context, appmodel.UnbilledProjectQuery) ([]model.UnbilledProject, error)
}

type Dependencies struct {
	Projects    ProjectReader
	Teams       TeamSettingsReader
	Memberships TeamMembershipReader
	Invoicing   InvoiceHistoryReader
	Decorations SessionDecorationBuilder
}

var ErrIncompleteDependencies = errors.New("project page builder dependencies are incomplete")

type Builder struct {
	projects    ProjectReader
	teams       TeamSettingsReader
	members     TeamMembershipReader
	invoicing   InvoiceHistoryReader
	decorations SessionDecorationBuilder
}

func New(deps Dependencies) (*Builder, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"project reader", deps.Projects},
		{"team settings reader", deps.Teams},
		{"team membership reader", deps.Memberships},
		{"invoice history reader", deps.Invoicing},
		{"session decoration builder", deps.Decorations},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{
		projects: deps.Projects, teams: deps.Teams, members: deps.Memberships,
		invoicing: deps.Invoicing, decorations: deps.Decorations,
	}, nil
}

func (b *Builder) Build(ctx context.Context, request appmodel.ProjectPageRequest) (appmodel.ProjectPageSnapshot, error) {
	if request.TeamID <= 0 {
		return appmodel.ProjectPageSnapshot{}, model.ErrNotFound
	}
	detail, err := b.projects.Detail(ctx, appmodel.ProjectDetailRequest{
		TeamID: request.TeamID, Slug: request.Slug, IncludeArchived: request.IncludeArchived,
		From: request.From, Through: request.Through,
	})
	if err != nil {
		return appmodel.ProjectPageSnapshot{}, fmt.Errorf("load project detail: %w", err)
	}
	currency, err := b.teams.Currency(ctx, request.TeamID)
	if err != nil {
		return appmodel.ProjectPageSnapshot{}, fmt.Errorf("load workspace currency: %w", err)
	}
	snapshot := appmodel.ProjectPageSnapshot{Detail: detail, TeamCurrency: currency}
	recent := detail.Activity.Recent
	if len(recent) > 50 {
		recent = recent[:50]
	}
	decorations, err := b.decorations.Build(ctx, appmodel.SessionDecorationRequest{
		TeamID: request.TeamID, Sessions: recent,
		IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		return appmodel.ProjectPageSnapshot{}, fmt.Errorf("build project session decorations: %w", err)
	}
	snapshot.TagsBySession, snapshot.ProjectsByID = decorations.TagsBySession, decorations.ProjectsByID
	if request.IncludeUnbilled {
		if request.CallerID <= 0 {
			return appmodel.ProjectPageSnapshot{}, model.ErrNotFound
		}
		role, member, err := b.members.IsMember(ctx, request.TeamID, request.CallerID)
		if err != nil {
			return appmodel.ProjectPageSnapshot{}, fmt.Errorf("resolve project page member role: %w", err)
		}
		if !member {
			return appmodel.ProjectPageSnapshot{}, model.ErrForbidden
		}
		if !role.CanManage() {
			return snapshot, nil
		}
		projectID := detail.Project.ID
		unbilled, err := b.invoicing.UnbilledProjectTime(ctx, appmodel.UnbilledProjectQuery{TeamID: request.TeamID, ProjectID: &projectID})
		if err != nil {
			return appmodel.ProjectPageSnapshot{}, fmt.Errorf("load project unbilled time: %w", err)
		}
		snapshot.Unbilled = unbilled
	}
	return snapshot, nil
}
