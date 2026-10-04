// Package memberadmin assembles the scoped read model for team member
// administration without coupling the teams and payroll workflows.
package memberadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type MemberReader interface {
	Members(context.Context, int64) ([]model.TeamMember, error)
}

type PayrollSettingsReader interface {
	MemberSettings(context.Context, int64) ([]model.MemberPayrollSettings, error)
}

type Dependencies struct {
	Members MemberReader
	Payroll PayrollSettingsReader
}

type Service struct {
	members MemberReader
	payroll PayrollSettingsReader
}

var ErrIncompleteDependencies = errors.New("member administration dependencies are incomplete")

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Members) || depcheck.IsNil(deps.Payroll) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{members: deps.Members, payroll: deps.Payroll}, nil
}

func (s *Service) Management(ctx context.Context, teamID int64) (appmodel.TeamMemberManagementSnapshot, error) {
	if teamID <= 0 {
		return appmodel.TeamMemberManagementSnapshot{}, model.ErrNotFound
	}
	members, err := s.members.Members(ctx, teamID)
	if err != nil {
		return appmodel.TeamMemberManagementSnapshot{}, fmt.Errorf("list team members: %w", err)
	}
	paySettings, err := s.payroll.MemberSettings(ctx, teamID)
	if err != nil {
		return appmodel.TeamMemberManagementSnapshot{}, fmt.Errorf("load team member payroll settings: %w", err)
	}
	return appmodel.TeamMemberManagementSnapshot{Members: members, PaySettings: paySettings}, nil
}
