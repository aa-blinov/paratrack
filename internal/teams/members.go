package teams

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// IsMember reports whether userID belongs to teamID and returns their role.
func (s *Service) IsMember(ctx context.Context, teamID, userID int64) (Role, bool, error) {
	if teamID <= 0 || userID <= 0 {
		return "", false, ErrNotFound
	}
	role, ok, err := s.memberships.TeamMemberRole(ctx, teamID, userID)
	if err != nil {
		return "", false, err
	}
	return role, ok, nil
}

// Members lists every member of the team, joined with user details so
// the UI can render "Alice (alice@example.com)" without an N+1 query.
// Owners are listed first, then by joined_at.
func (s *Service) Members(ctx context.Context, teamID int64) ([]Member, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	return s.memberships.ListTeamMembers(ctx, teamID)
}

// SetRole changes a member's role. Only the owner decides who manages
// money and settings; the owner role itself isn't handed out here.
func (s *Service) SetRole(ctx context.Context, request appmodel.TeamMemberRoleRequest) error {
	if request.TeamID <= 0 {
		return ErrNotFound
	}
	targetUserID, callerID, role := request.TargetUserID, request.CallerID, Role(request.Role)
	if callerID <= 0 || targetUserID <= 0 || callerID == targetUserID {
		return ErrForbidden
	}
	if role != RoleAdmin && role != RoleMember {
		return fmt.Errorf("%w: role must be admin or member", ErrValidation)
	}
	updated, err := s.memberships.SetTeamMemberRole(ctx, request)
	if err != nil {
		return mapStoreError(err)
	}
	if !updated {
		return ErrForbidden
	}
	return nil
}

// TransferOwnership hands the workspace to another member. Only the owner
// can, the new owner must already be in the team, and the old owner stays
// on as an admin. A personal workspace isn't handed over.
func (s *Service) TransferOwnership(ctx context.Context, request appmodel.TeamOwnershipTransferRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.NewOwnerID <= 0 {
		return ErrNotFound
	}
	teamID, callerID, newOwnerID := request.TeamID, request.CallerID, request.NewOwnerID
	t, err := s.FindByID(ctx, teamID)
	if err != nil {
		return err
	}
	if t.OwnerID != callerID || callerID == newOwnerID {
		return ErrForbidden
	}
	if strings.HasPrefix(t.Slug, fmt.Sprintf("personal-%d-", callerID)) {
		return fmt.Errorf("%w: a personal workspace can't be handed over", ErrValidation)
	}
	_, ok, err := s.IsMember(ctx, teamID, newOwnerID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if err := s.memberships.TransferTeamOwnership(ctx, request); err != nil {
		return mapStoreForbidden(err)
	}
	return nil
}

// RemoveMember drops someone from the team. Owners can remove anyone; members
// can only remove themselves (leave). Returns ErrForbidden if the rules don't
// fit the caller's role.
func (s *Service) RemoveMember(ctx context.Context, request appmodel.TeamMemberRemovalRequest) error {
	if request.TeamID <= 0 || request.TargetUserID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	teamID, targetUserID, callerID := request.TeamID, request.TargetUserID, request.CallerID
	callerRole, isCaller, err := s.IsMember(ctx, teamID, callerID)
	if err != nil {
		return err
	}
	if !isCaller {
		return ErrForbidden
	}
	if callerID == targetUserID {
		// Self-leave: members can always leave their own teams.
	} else if !callerRole.CanManage() {
		return ErrForbidden
	} else {
		targetRole, isTarget, err := s.IsMember(ctx, teamID, targetUserID)
		if err != nil {
			return err
		}
		if isTarget && targetRole == RoleOwner && callerRole != RoleOwner {
			return ErrForbidden // an admin can't remove the owner
		}
	}
	if callerID == targetUserID {
		// Last owner can't leave — they have to delete the team.
		t, err := s.FindByID(ctx, teamID)
		if err != nil {
			return err
		}
		if t.OwnerID == callerID {
			ownerCount, err := s.memberships.CountTeamOwners(ctx, teamID)
			if err != nil {
				return err
			}
			if ownerCount <= 1 {
				return fmt.Errorf("%w: last owner must delete the team", ErrValidation)
			}
		}
	}
	// Preserve payroll history, stop the member's timers, and remove team push
	// subscriptions atomically. The store rechecks authorization under locks.
	request.LeftAt = s.now().UTC()
	err = s.memberships.RemoveTeamMember(ctx, request)
	if errors.Is(err, model.ErrLastOwner) {
		return fmt.Errorf("%w: last owner must delete the team", ErrValidation)
	}
	if errors.Is(err, model.ErrOwnerMustTransfer) {
		return fmt.Errorf("%w: transfer ownership before removing the current owner", ErrValidation)
	}
	return mapStoreError(err)
}
