package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/trackingops"
)

// cliWorkspace resolves the trusted local operator's default workspace and
// owner without adding these capabilities to feature workflows.
type cliWorkspaceStore interface {
	FirstTeamID(context.Context) (int64, error)
	TeamOwnerID(context.Context, int64) (int64, error)
}

type cliWorkspace struct{ store cliWorkspaceStore }

func (w cliWorkspace) DefaultTeam(ctx context.Context) (int64, error) {
	return w.store.FirstTeamID(ctx)
}

func (w cliWorkspace) TeamOwnerID(ctx context.Context, teamID int64) (int64, error) {
	if teamID <= 0 {
		return 0, model.ErrNotFound
	}
	return w.store.TeamOwnerID(ctx, teamID)
}

type cliProjectLookupStore interface {
	ProjectIDBySlug(context.Context, string) (int64, error)
	GetProjectByID(context.Context, int64) (model.Project, error)
}

type cliProjectLookup struct{ store cliProjectLookupStore }

func (p cliProjectLookup) ProjectIDBySlug(ctx context.Context, slug string) (int64, error) {
	return p.store.ProjectIDBySlug(ctx, slug)
}

func (p cliProjectLookup) GetProjectByID(ctx context.Context, id int64) (model.Project, error) {
	if id <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	return p.store.GetProjectByID(ctx, id)
}

// NewCLIServices constructs the workflows exposed by CLI commands. It wires
// push delivery for timer notifications while keeping HTTP and webhook
// delivery clients out of the command process.
func NewCLIServices(database *db.DB, config CLIConfig) (*cliport.Services, io.Closer, error) {
	if database == nil {
		return nil, nil, ErrNilDatabase
	}
	if err := validateCLIConfig(config); err != nil {
		return nil, nil, err
	}
	logger := config.Logger
	shared, err := newSharedWorkflows(database)
	if err != nil {
		return nil, nil, err
	}
	resources := &applicationResources{}
	pushService, err := newPushService(database, logger, shared.Audit, resources)
	if err != nil {
		return nil, nil, errors.Join(err, resources.Close())
	}
	trackingOpsService, err := trackingops.New(trackingops.Dependencies{
		Sessions: shared.Tracking, Activities: shared.Tracking, Resolver: shared.Tracking,
		Projects: shared.Projects, ClosedSessions: shared.Tracking, Goals: shared.Goals,
		Audit:         shared.Audit,
		Notifications: trackingPushNotifications{push: pushService}, Logger: logger,
	})
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("construct CLI tracking operations: %w", err), resources.Close())
	}
	return &cliport.Services{
		Workspace:       cliWorkspace{store: database},
		ProjectLookup:   cliProjectLookup{store: database},
		Projects:        shared.Projects,
		TimerQueries:    shared.Tracking,
		TimerCommands:   shared.Tracking,
		TimerOperations: trackingOpsService,
		ActivityCatalog: shared.Tracking,
		SessionHistory:  shared.Tracking,
		Tagging:         shared.Tagging,
		Goals:           shared.Goals,
	}, resources, nil
}
