// Package agentactions queues the four actions an agent may request but never run:
// deleting a draft invoice or pay run, reopening an approved week and deleting the
// workspace. A request waits here until a person confirms or rejects it in the app.
// Who may request and who may decide is checked in the persistence transaction.
package agentactions

import (
	"context"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
)

// Store is the persistence contract, satisfied by the database adapter.
type Store interface {
	CreateAgentAction(context.Context, appmodel.AgentActionCreateRequest) (appmodel.AgentActionView, error)
	ListPendingAgentActions(context.Context, appmodel.AgentActionListQuery) ([]appmodel.AgentActionView, error)
	ListAgentActionJournal(context.Context, appmodel.AgentActionListQuery) ([]appmodel.AgentActionView, error)
	DecideAgentAction(context.Context, appmodel.AgentActionDecisionRequest) (appmodel.AgentActionView, error)
	FinishAgentAction(context.Context, appmodel.AgentActionFinishRequest) error
}

// Service is the queue the transports use. Running a confirmed action is not its job:
// the transport runs the same service the page runs for that kind, then reports the outcome.
type Service struct {
	store Store
}

var ErrIncompleteDependencies = errors.New("agent action queue dependencies are incomplete")

func New(store Store) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store}, nil
}

// Request queues an action for a person to confirm.
func (s *Service) Request(ctx context.Context, request appmodel.AgentActionCreateRequest) (appmodel.AgentActionView, error) {
	if request.TeamID <= 0 {
		return appmodel.AgentActionView{}, appmodel.ErrInvalidWorkspace
	}
	return s.store.CreateAgentAction(ctx, request)
}

// Journal lists the requests a decision has settled, so a refused confirmation stays visible
// with its reason.
func (s *Service) Journal(ctx context.Context, query appmodel.AgentActionListQuery) ([]appmodel.AgentActionView, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.ListAgentActionJournal(ctx, query)
}

// Pending lists the requests still waiting for a decision.
func (s *Service) Pending(ctx context.Context, query appmodel.AgentActionListQuery) ([]appmodel.AgentActionView, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.ListPendingAgentActions(ctx, query)
}

// Decide confirms (claims) or rejects a pending request.
func (s *Service) Decide(ctx context.Context, request appmodel.AgentActionDecisionRequest) (appmodel.AgentActionView, error) {
	if request.TeamID <= 0 {
		return appmodel.AgentActionView{}, appmodel.ErrInvalidWorkspace
	}
	return s.store.DecideAgentAction(ctx, request)
}

// Finish records how a confirmed request ended.
func (s *Service) Finish(ctx context.Context, request appmodel.AgentActionFinishRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidWorkspace
	}
	return s.store.FinishAgentAction(ctx, request)
}
