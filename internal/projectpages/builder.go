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

type TeamSettingsReader interface {
	Currency(context.Context, int64) (string, error)
}

type TeamMembershipReader interface {
	IsMember(context.Context, int64, int64) (model.TeamRole, bool, error)
}

type InvoiceHistoryReader interface {
	UnbilledProjectTime(context.Context, int64, int64) ([]model.UnbilledProject, error)
}

type Dependencies struct {
	Projects    ProjectReader
	Teams       TeamSettingsReader
	Memberships TeamMembershipReader
	Invoicing   InvoiceHistoryReader
}

var ErrIncompleteDependencies = errors.New("project page builder dependencies are incomplete")

type Builder struct {
	projects  ProjectReader
	teams     TeamSettingsReader
	members   TeamMembershipReader
	invoicing InvoiceHistoryReader
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
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{projects: deps.Projects, teams: deps.Teams, members: deps.Memberships, invoicing: deps.Invoicing}, nil
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
		unbilled, err := b.invoicing.UnbilledProjectTime(ctx, request.TeamID, detail.Project.ID)
		if err != nil {
			return appmodel.ProjectPageSnapshot{}, fmt.Errorf("load project unbilled time: %w", err)
		}
		snapshot.Unbilled = unbilled
	}
	return snapshot, nil
}
