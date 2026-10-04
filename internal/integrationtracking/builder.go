// Package integrationtracking coordinates imported-task lookup with timer
// creation without coupling either feature workflow to the other.
package integrationtracking

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type TaskReader interface {
	Task(context.Context, appmodel.ExternalTaskLookupQuery) (model.ExternalTask, error)
}

type TimerStarter interface {
	StartActivity(context.Context, appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error)
}

type Dependencies struct {
	Tasks  TaskReader
	Timers TimerStarter
}

var ErrIncompleteDependencies = errors.New("integration task tracking dependencies are incomplete")

type Builder struct {
	tasks  TaskReader
	timers TimerStarter
}

func New(deps Dependencies) (*Builder, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"imported task reader", deps.Tasks},
		{"timer starter", deps.Timers},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{tasks: deps.Tasks, timers: deps.Timers}, nil
}

// Start loads a workspace-owned imported task and starts a timer using its
// title as the activity name. Tracking remains responsible for resolving the
// activity, optional project assignment, authorization and audit effects.
func (b *Builder) Start(ctx context.Context, request appmodel.ImportedTaskStartRequest) (model.Activity, model.Session, error) {
	if request.TeamID <= 0 || request.TaskID <= 0 {
		return model.Activity{}, model.Session{}, model.ErrNotFound
	}
	task, err := b.tasks.Task(ctx, appmodel.ExternalTaskLookupQuery{TeamID: request.TeamID, TaskID: request.TaskID})
	if err != nil {
		return model.Activity{}, model.Session{}, fmt.Errorf("load imported task %d: %w", request.TaskID, err)
	}
	activity, session, err := b.timers.StartActivity(ctx, appmodel.TimerStartByNameRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, ActivityName: task.Title,
		ProjectID: request.ProjectID, At: request.At, Note: request.Note,
	})
	if err != nil {
		return activity, model.Session{}, err
	}
	return activity, session, nil
}
