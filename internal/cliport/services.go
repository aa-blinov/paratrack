// Package cliport defines the narrow application contract used by the CLI adapter.
package cliport

import (
	"context"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

var ErrIncompleteServices = errors.New("CLI application services are incomplete")

// Services contains only workflows used by CLI commands.
type Services struct {
	TimerQueries    TimerQueries
	TimerCommands   TimerCommands
	TimerOperations TimerOperations
	ActivityCatalog ActivityCatalog
	SessionHistory  SessionHistory
	Workspace       WorkspaceContext
	ProjectLookup   ProjectLookup
	ProjectQueries  ProjectQueries
	ProjectCommands ProjectCommands
	TagQueries      TagQueries
	TagCommands     TagCommands
	GoalQueries     GoalQueries
	GoalCommands    GoalCommands
}

func (s *Services) Validate() error {
	if s == nil || depcheck.IsNil(s.TimerQueries) || depcheck.IsNil(s.TimerCommands) || depcheck.IsNil(s.TimerOperations) || depcheck.IsNil(s.ActivityCatalog) ||
		depcheck.IsNil(s.SessionHistory) || depcheck.IsNil(s.Workspace) || depcheck.IsNil(s.ProjectLookup) ||
		depcheck.IsNil(s.ProjectQueries) || depcheck.IsNil(s.ProjectCommands) ||
		depcheck.IsNil(s.TagQueries) || depcheck.IsNil(s.TagCommands) ||
		depcheck.IsNil(s.GoalQueries) || depcheck.IsNil(s.GoalCommands) {
		return ErrIncompleteServices
	}
	return nil
}

// TimerOperations contains transitions that coordinate audit and webhook
// effects. Active-session reads and pause/resume remain on the lower-level
// timer ports.
type TimerOperations interface {
	AddClosed(context.Context, appmodel.TimerAddByIDRequest) (model.Session, error)
	FocusActivityForMember(context.Context, appmodel.TimerFocusForMemberRequest) (model.Activity, appmodel.FocusResult, error)
	StartActivity(context.Context, appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error)
	Stop(context.Context, appmodel.TimerStopRequest) (appmodel.TimerStopResult, error)
	StopAll(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
}

// SessionHistory is the read-only tracking capability used by CLI reports.
type SessionHistory interface {
	ClosedSessions(context.Context, appmodel.ClosedSessionsQuery) ([]model.ActiveSession, error)
}

// ActivityCatalog supplies the activity selection and creation operations
// needed by interactive timer commands.
type ActivityCatalog interface {
	Activities(context.Context, int64, bool) ([]model.Activity, error)
	FindActivity(context.Context, appmodel.ActivityNameQuery) (model.Activity, error)
	ResolveActivityForMember(context.Context, appmodel.ActivityResolveRequest) (model.Activity, error)
}

// TimerQueries exposes the active-session read used by timer commands.
type TimerQueries interface {
	ActiveSessions(context.Context, int64) ([]model.ActiveSession, error)
}

// TimerCommands contains pause and resume transitions. Start, focus and stop
// operations that coordinate audit and notifications live on TimerOperations.
type TimerCommands interface {
	PauseAll(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
	Pause(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
	Resume(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
}

// WorkspaceContext resolves the trusted local CLI principal. User-facing
// project operations remain workspace-scoped.
type WorkspaceContext interface {
	DefaultTeam(context.Context) (int64, error)
	TeamOwnerID(context.Context, int64) (int64, error)
}

// ProjectLookup supports local operator commands that resolve a project across
// workspaces. Normal project operations remain on the scoped Projects port.
type ProjectLookup interface {
	ProjectIDBySlug(context.Context, string) (int64, error)
	GetProjectByID(context.Context, int64) (model.Project, error)
}

// ProjectQueries supplies workspace-scoped project catalog reads.
type ProjectQueries interface {
	ListWithActivityCounts(context.Context, appmodel.ProjectCatalogQuery) (appmodel.ProjectCatalogSnapshot, error)
	Activities(context.Context, appmodel.ProjectActivityCatalogQuery) ([]model.Activity, error)
}

// ProjectCommands supplies workspace-scoped project mutations.
type ProjectCommands interface {
	Create(context.Context, appmodel.ProjectCreateRequest) (model.Project, error)
	Update(context.Context, appmodel.ProjectUpdateRequest) (model.Project, error)
	Delete(context.Context, appmodel.ProjectMutationRequest) error
}

// TagQueries supplies tag catalogs for CLI output.
type TagQueries interface {
	ListWithCounts(context.Context, appmodel.TagListQuery) ([]appmodel.TagWithCount, error)
}

// TagCommands creates tags and changes session assignments.
type TagCommands interface {
	CreateForMember(context.Context, appmodel.TagCreateRequest) (model.Tag, error)
	AttachForMember(context.Context, appmodel.SessionTagRequest) error
	DetachForMember(context.Context, appmodel.SessionTagRequest) error
}

// GoalQueries supplies current goal progress for CLI output.
type GoalQueries interface {
	Progress(context.Context, appmodel.GoalProgressQuery) ([]appmodel.GoalProgress, error)
}

// GoalCommands creates and removes manager-owned goal targets.
type GoalCommands interface {
	SetForManager(context.Context, appmodel.GoalSetRequest) ([]model.Goal, error)
	UnsetForManager(context.Context, appmodel.GoalUnsetRequest) (int, error)
}
