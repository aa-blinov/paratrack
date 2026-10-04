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
	invite := Invite{Token: token, TeamID: request.TeamID, Role: RoleMember, CreatedAt: now, ExpiresAt: expires}
	if err := s.invites.CreateTeamInvite(ctx, appmodel.TeamInvitePersistenceRequest{
		Token: invite.Token, TeamID: invite.TeamID, Role: invite.Role,
		CallerID: request.CallerID, CreatedAt: invite.CreatedAt, ExpiresAt: invite.ExpiresAt,
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
	if err != nil {
		return Invite{}, mapStoreError(err)
	}
	return inviteResult(invite), nil
}

// InvitePage assembles public invitation details. Missing invitations or
// deleted workspaces produce the empty state; operational failures propagate.
func (s *Service) InvitePage(ctx context.Context, token string) (appmodel.TeamInvitePageSnapshot, error) {
	invite, err := s.FindInvite(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return appmodel.TeamInvitePageSnapshot{}, nil
	}
	if err != nil {
		return appmodel.TeamInvitePageSnapshot{}, fmt.Errorf("find invitation for page: %w", err)
	}
	team, err := s.FindByID(ctx, invite.TeamID)
	if errors.Is(err, ErrNotFound) {
		return appmodel.TeamInvitePageSnapshot{}, nil
	}
	if err != nil {
		return appmodel.TeamInvitePageSnapshot{}, fmt.Errorf("find invitation workspace: %w", err)
	}
	return appmodel.TeamInvitePageSnapshot{Invite: invite, Team: team}, nil
}

// InvitesForTeam lists every (live or dead) invite for a team.
func (s *Service) InvitesForTeam(ctx context.Context, teamID int64) ([]Invite, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	invites, err := s.invites.ListTeamInvites(ctx, teamID)
	if err != nil {
		return nil, err
	}
	results := make([]Invite, 0, len(invites))
	for _, invite := range invites {
		results = append(results, inviteResult(invite))
	}
	return results, nil
}

func inviteResult(invite model.TeamInvite) Invite {
	return Invite{
		Token: invite.Token, TeamID: invite.TeamID, Role: invite.Role,
		CreatedAt: invite.CreatedAt, ExpiresAt: invite.ExpiresAt,
		AcceptedAt: invite.AcceptedAt,
	}
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
