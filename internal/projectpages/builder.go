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
	Summaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error)
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

type SessionTagReader interface {
	TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error)
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Projects    ProjectReader
	Teams       TeamSettingsReader
	Memberships TeamMembershipReader
	Invoicing   InvoiceHistoryReader
	Tags        SessionTagReader
	Logger      Logger
}

var ErrIncompleteDependencies = errors.New("project page builder dependencies are incomplete")

type Builder struct {
	projects  ProjectReader
	teams     TeamSettingsReader
	members   TeamMembershipReader
	invoicing InvoiceHistoryReader
	tags      SessionTagReader
	logger    Logger
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
		{"session tag reader", deps.Tags},
		{"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{
		projects: deps.Projects, teams: deps.Teams, members: deps.Memberships,
		invoicing: deps.Invoicing, tags: deps.Tags, logger: deps.Logger,
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
	sessionIDs, projectIDs := collectSessionReferences(detail.Activity.Recent)
	if len(sessionIDs) > 0 {
		tags, err := b.tags.TagsForSessions(ctx, request.TeamID, sessionIDs)
		if err != nil {
			b.logger.Printf("projectpages: load session tags for team %d: %v", request.TeamID, err)
		} else {
			snapshot.TagsBySession = tags
		}
	}
	if len(projectIDs) > 0 {
		summaries, err := b.projects.Summaries(ctx, request.TeamID, projectIDs)
		if err != nil {
			b.logger.Printf("projectpages: load project summaries for team %d: %v", request.TeamID, err)
		} else {
			snapshot.ProjectsByID = summaries
		}
	}
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

func collectSessionReferences(sessions []model.ActiveSession) ([]int64, []int64) {
	if len(sessions) > 50 {
		sessions = sessions[:50]
	}
	var sessionIDs, projectIDs []int64
	seenSessions, seenProjects := map[int64]bool{}, map[int64]bool{}
	for _, item := range sessions {
		if item.Session.ID > 0 && !seenSessions[item.Session.ID] {
			seenSessions[item.Session.ID] = true
			sessionIDs = append(sessionIDs, item.Session.ID)
		}
		projectID := item.Activity.ProjectID
		if projectID > 0 && !seenProjects[projectID] {
			seenProjects[projectID] = true
			projectIDs = append(projectIDs, projectID)
		}
	}
	return sessionIDs, projectIDs
}
