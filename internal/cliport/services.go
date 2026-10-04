// Package cliport defines the narrow application contract used by the CLI adapter.
package cliport

import (
	"context"
	"errors"
	"time"

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
	Projects        Projects
	Tagging         Tagging
	Goals           Goals
}

func (s *Services) Validate() error {
	if s == nil || depcheck.IsNil(s.TimerQueries) || depcheck.IsNil(s.TimerCommands) || depcheck.IsNil(s.TimerOperations) || depcheck.IsNil(s.ActivityCatalog) ||
		depcheck.IsNil(s.SessionHistory) || depcheck.IsNil(s.Workspace) || depcheck.IsNil(s.ProjectLookup) || depcheck.IsNil(s.Projects) ||
		depcheck.IsNil(s.Tagging) || depcheck.IsNil(s.Goals) {
		return ErrIncompleteServices
	}
	return nil
}

// TimerOperations contains transitions that coordinate audit and webhook
// effects. Active-session reads and pause/resume remain on the lower-level
// timer ports.
type TimerOperations interface {
	AddClosed(context.Context, appmodel.TimerAddByIDRequest) (model.Session, error)
	Focus(context.Context, appmodel.TimerFocusRequest) (appmodel.FocusResult, error)
	StartActivity(context.Context, appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error)
	Stop(context.Context, appmodel.TimerStopRequest) (appmodel.TimerStopResult, error)
	StopAll(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
}

// SessionHistory is the read-only tracking capability used by CLI reports.
type SessionHistory interface {
	ClosedSessions(context.Context, int64, time.Time, time.Time, *int64) ([]model.ActiveSession, error)
}

// ActivityCatalog supplies the activity selection and creation operations
// needed by interactive timer commands.
type ActivityCatalog interface {
	Activities(context.Context, int64, bool) ([]model.Activity, error)
	FindActivity(context.Context, int64, string) (model.Activity, error)
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

type Projects interface {
	ListWithActivityCounts(context.Context, int64, bool) (appmodel.ProjectCatalogSnapshot, error)
	Create(context.Context, appmodel.ProjectCreateRequest) (model.Project, error)
	Activities(context.Context, int64, int64, bool) ([]model.Activity, error)
	Update(context.Context, appmodel.ProjectUpdateRequest) (model.Project, error)
	Delete(context.Context, appmodel.ProjectMutationRequest) error
}

type Tagging interface {
	CreateForMember(context.Context, appmodel.TagCreateRequest) (model.Tag, error)
	ListWithCounts(context.Context, int64) ([]model.TagWithCount, error)
	AttachForMember(context.Context, appmodel.SessionTagRequest) error
	DetachForMember(context.Context, appmodel.SessionTagRequest) error
}

type Goals interface {
	UpsertForManager(context.Context, appmodel.GoalUpsertRequest) (model.Goal, error)
	Progress(context.Context, int64, time.Time) ([]model.GoalProgress, error)
	DeleteForManager(context.Context, appmodel.GoalDeleteRequest) error
}
