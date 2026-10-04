package teams

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// NewInvite creates a single-use token that expires after seven days.
func (s *Service) NewInvite(ctx context.Context, request appmodel.TeamInviteCreateRequest) (Invite, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return Invite{}, ErrNotFound
	}
	role, isCaller, err := s.IsMember(ctx, request.TeamID, request.CallerID)
	if err != nil {
		return Invite{}, err
	}
	if !isCaller || !role.CanManage() {
		return Invite{}, ErrForbidden
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Invite{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now().UTC()
	expires := now.Add(7 * 24 * time.Hour)
	invite := Invite{Token: token, TeamID: request.TeamID, Role: RoleMember, CreatedBy: request.CallerID, CreatedAt: now, ExpiresAt: expires}
	if err := s.invites.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{
		Token: invite.Token, TeamID: invite.TeamID, Role: invite.Role,
		CallerID: invite.CreatedBy, CreatedAt: invite.CreatedAt, ExpiresAt: invite.ExpiresAt,
	}); err != nil {
		return Invite{}, mapStoreError(err)
	}
	return invite, nil
}

// FindInvite looks up an invite by its token. Doesn't check expiry or
// already-accepted state — callers should consult ExpiredAt / Accepted
// fields on the result.
func (s *Service) FindInvite(ctx context.Context, token string) (Invite, error) {
	if token == "" {
		return Invite{}, ErrNotFound
	}
	invite, err := s.invites.FindTeamInvite(ctx, token)
	return invite, mapStoreError(err)
}

// InvitesForTeam lists every (live or dead) invite for a team.
func (s *Service) InvitesForTeam(ctx context.Context, teamID int64) ([]Invite, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	return s.invites.ListTeamInvites(ctx, teamID)
}

// RevokeInvite deletes an invite. Only workspace managers can revoke.
func (s *Service) RevokeInvite(ctx context.Context, request appmodel.TeamInviteRevokeRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.Token == "" {
		return ErrNotFound
	}
	role, isCaller, err := s.IsMember(ctx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	if !isCaller || !role.CanManage() {
		return ErrForbidden
	}
	return mapStoreError(s.invites.DeleteTeamInvite(ctx, request))
}

// AcceptInvite validates the token, marks it accepted, and adds the caller
// as a member of the team. Returns the team they joined.
func (s *Service) AcceptInvite(ctx context.Context, request appmodel.TeamInviteAcceptRequest) (Team, error) {
	token, userID := request.Token, request.UserID
	if userID <= 0 {
		return Team{}, ErrNotFound
	}
	inv, err := s.FindInvite(ctx, token)
	if err != nil {
		return Team{}, err
	}
	if !inv.AcceptedAt.IsZero() {
		return Team{}, fmt.Errorf("%w: invite already used", ErrValidation)
	}
	now := s.now().UTC()
	if inv.ExpiredAt(now) {
		return Team{}, fmt.Errorf("%w: invite expired", ErrValidation)
	}
	if inv.TeamID <= 0 {
		return Team{}, ErrNotFound
	}
	if err := s.invites.AcceptTeamInvite(ctx, appmodel.TeamInviteAcceptanceRequest{
		Token: request.Token, TeamID: inv.TeamID, UserID: request.UserID, At: now,
	}); err != nil {
		if errors.Is(err, model.ErrInviteExpired) {
			return Team{}, fmt.Errorf("%w: invite expired", ErrValidation)
		}
		if errors.Is(err, model.ErrNotFound) {
			return Team{}, fmt.Errorf("%w: invite already used", ErrValidation)
		}
		return Team{}, err
	}
	return s.FindByID(ctx, inv.TeamID)
}
