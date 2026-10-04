// Package teamops coordinates workspace mutations with audit and account
// lifecycle side effects.
package teamops

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrIncompleteDependencies = errors.New("team operations dependencies are incomplete")

type Mutations interface {
	SetRole(context.Context, appmodel.TeamMemberRoleRequest) error
	SetStripeCredentials(context.Context, appmodel.TeamStripeCredentialsRequest) error
	TransferOwnership(context.Context, appmodel.TeamOwnershipTransferRequest) error
	UpdateBilling(context.Context, appmodel.TeamBillingRequest) error
	UpdateCurrency(context.Context, appmodel.TeamCurrencyRequest) error
}

type WorkspaceLifecycle interface {
	DeleteAndListRemaining(context.Context, appmodel.WorkspaceDeleteRequest) ([]int64, error)
}

type SessionRevoker interface {
	DeleteByUser(context.Context, int64) error
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Mutations Mutations
	Workspace WorkspaceLifecycle
	Sessions  SessionRevoker
	Audit     AuditRecorder
	Logger    Logger
}

type Service struct {
	mutations Mutations
	workspace WorkspaceLifecycle
	sessions  SessionRevoker
	audit     AuditRecorder
	logger    Logger
}

func New(deps Dependencies) (*Service, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"team mutations", deps.Mutations},
		{"workspace lifecycle", deps.Workspace},
		{"session revoker", deps.Sessions},
		{"audit recorder", deps.Audit},
		{"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{mutations: deps.Mutations, workspace: deps.Workspace, sessions: deps.Sessions, audit: deps.Audit, logger: deps.Logger}, nil
}

// DeleteWorkspace removes the workspace and revokes the caller's sessions if
// no workspace memberships remain. Session revocation preserves the prior
// best-effort behavior because the workspace deletion has already committed.
func (s *Service) DeleteWorkspace(ctx context.Context, request appmodel.WorkspaceDeleteRequest) ([]int64, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return nil, model.ErrNotFound
	}
	callerID := request.CallerID
	remaining, err := s.workspace.DeleteAndListRemaining(ctx, request)
	if err != nil {
		return nil, err
	}
	if len(remaining) == 0 {
		effectCtx, cancel := postcommit.NewContext(ctx)
		defer cancel()
		if err := s.sessions.DeleteByUser(effectCtx, callerID); err != nil {
			s.logger.Printf("teamops: could not revoke sessions for user %d after workspace deletion: %v", callerID, err)
		}
	}
	return remaining, nil
}

func (s *Service) UpdateCurrency(ctx context.Context, request appmodel.TeamCurrencyRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if err := s.mutations.UpdateCurrency(ctx, request); err != nil {
		return err
	}
	s.record(ctx, request.TeamID, request.CallerID, "team.currency", request.Currency, "")
	return nil
}

func (s *Service) SetRole(ctx context.Context, request appmodel.TeamMemberRoleRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TargetUserID <= 0 {
		return model.ErrNotFound
	}
	if err := s.mutations.SetRole(ctx, request); err != nil {
		return err
	}
	s.record(ctx, request.TeamID, request.CallerID, "member.role", strconv.FormatInt(request.TargetUserID, 10), string(request.Role))
	return nil
}

func (s *Service) TransferOwnership(ctx context.Context, request appmodel.TeamOwnershipTransferRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.NewOwnerID <= 0 {
		return model.ErrNotFound
	}
	if err := s.mutations.TransferOwnership(ctx, request); err != nil {
		return err
	}
	s.record(ctx, request.TeamID, request.CallerID, "team.transfer", strconv.FormatInt(request.NewOwnerID, 10), "")
	return nil
}

func (s *Service) UpdateBilling(ctx context.Context, request appmodel.TeamBillingRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if err := s.mutations.UpdateBilling(ctx, request); err != nil {
		return err
	}
	meta := fmt.Sprintf("%d %s %s", request.Rules.RoundMinutes, request.Rules.RoundMode, request.Rules.InvoicePrefix)
	s.record(ctx, request.TeamID, request.CallerID, "team.billing", meta, "")
	return nil
}

func (s *Service) SetStripeCredentials(ctx context.Context, request appmodel.TeamStripeCredentialsRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if err := s.mutations.SetStripeCredentials(ctx, request); err != nil {
		return err
	}
	s.record(ctx, request.TeamID, request.CallerID, "team.stripe_update", "", "")
	return nil
}

func (s *Service) record(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("teamops: record %s audit for team %d: %v", action, teamID, err)
	}
}
