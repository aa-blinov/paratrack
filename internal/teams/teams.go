// Package teams owns team, membership and invite operations.
package teams

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teamslug"
)

type Role = model.TeamRole

const (
	RoleOwner  = model.TeamRoleOwner
	RoleAdmin  = model.TeamRoleAdmin
	RoleMember = model.TeamRoleMember
)

type Team = model.Team
type Member = model.TeamMember
type Invite = appmodel.TeamInviteResult

// TeamStore provides core workspace operations.
type TeamStore interface {
	CreateOwnedTeam(context.Context, appmodel.TeamCreateRequest) (int64, error)
	FindTeam(context.Context, int64) (model.Team, error)
	FindTeams(context.Context, []int64) (map[int64]model.Team, error)
	FindTeamBySlug(context.Context, string) (model.Team, error)
	ListTeamsForUser(context.Context, int64) ([]model.Team, error)
	RenameTeam(context.Context, appmodel.TeamRenameRequest) error
	DeleteTeamAndListRemaining(context.Context, appmodel.WorkspaceDeleteRequest) ([]model.Team, error)
}

// MembershipStore provides workspace membership operations.
type MembershipStore interface {
	ListMembershipsForUser(context.Context, int64) ([]model.TeamMembership, error)
	FindMembershipForUser(context.Context, appmodel.TeamMembershipQuery) (model.TeamMembership, bool, error)
	TeamMemberRole(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error)
	ListTeamMembers(context.Context, int64) ([]model.TeamMember, error)
	SetTeamMemberRole(context.Context, appmodel.TeamMemberRoleRequest) (bool, error)
	TransferTeamOwnership(context.Context, appmodel.TeamOwnershipTransferRequest) error
	CountTeamOwners(context.Context, int64) (int, error)
	RemoveTeamMember(context.Context, appmodel.TeamMemberRemovalRequest) error
}

// InviteStore provides workspace invitation operations.
type InviteStore interface {
	CreateTeamInvite(context.Context, appmodel.TeamInvitePersistenceRequest) error
	FindTeamInvite(context.Context, string) (model.TeamInvite, error)
	ListTeamInvites(context.Context, int64) ([]model.TeamInvite, error)
	DeleteTeamInvite(context.Context, appmodel.TeamInviteRevokeRequest) error
	AcceptTeamInvite(context.Context, appmodel.TeamInviteAcceptanceRequest) error
}

// SettingsStore provides billing and workspace profile settings.
type SettingsStore interface {
	TeamBilling(context.Context, int64) (model.BillingRules, error)
	TeamCurrency(context.Context, int64) (string, error)
	TeamRequisites(context.Context, int64) (string, string, error)
	SetTeamBilling(context.Context, appmodel.TeamBillingRequest) error
	SetTeamCurrency(context.Context, appmodel.TeamCurrencyRequest) error
	SetTeamRequisites(context.Context, appmodel.TeamRequisitesRequest) error
	SetTeamLogo(context.Context, appmodel.TeamLogoRequest) error
	SetTeamStripe(context.Context, appmodel.TeamStripeCredentialsRequest) error
}

// ModuleStore provides workspace module preference operations.
type ModuleStore interface {
	TeamModules(context.Context, int64) (string, error)
	SetTeamModules(context.Context, appmodel.TeamModulesRequest) error
}

// Dependencies keeps each workspace workflow on a resource-specific port.
type Dependencies struct {
	Teams       TeamStore
	Memberships MembershipStore
	Invites     InviteStore
	Settings    SettingsStore
	Modules     ModuleStore
	Now         func() time.Time
}

var ErrIncompleteDependencies = errors.New("workspace service dependencies are incomplete")

// Service contains workspace policy and coordinates persistence operations.
type Service struct {
	teams       TeamStore
	memberships MembershipStore
	invites     InviteStore
	settings    SettingsStore
	modules     ModuleStore
	now         func() time.Time
}

// NewService returns a Service bound to its persistence contract.
func NewService(deps Dependencies) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"teams", deps.Teams}, {"memberships", deps.Memberships},
		{"invites", deps.Invites}, {"settings", deps.Settings}, {"modules", deps.Modules},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	if deps.Now == nil {
		return nil, fmt.Errorf("%w: clock", ErrIncompleteDependencies)
	}
	return &Service{
		teams: deps.Teams, memberships: deps.Memberships,
		invites: deps.Invites, settings: deps.Settings, modules: deps.Modules,
		now: deps.Now,
	}, nil
}

// ErrNotFound / ErrDuplicate are returned by the typed lookups in this
// package; callers can match against them with errors.Is.
var (
	ErrNotFound        = appmodel.ErrTeamNotFound
	ErrDuplicate       = appmodel.ErrTeamDuplicate
	ErrValidation      = appmodel.ErrTeamValidation
	ErrForbidden       = appmodel.ErrTeamForbidden
	ErrInvalidBilling  = appmodel.ErrInvalidTeamBilling
	ErrInvalidSettings = appmodel.ErrInvalidTeamSettings
)

// Slugify turns a free-form team name into a URL-safe slug. Whitespace
// becomes hyphens, the rest is lowercased, and any non-alphanumeric
// characters are dropped. Empty slugs fall back to "team".
func Slugify(name string) string {
	return teamslug.Slugify(name)
}

// Create makes a brand-new team owned by ownerID. The slug is taken
// from the supplied name (Slugify) and an integer suffix is appended
// if it collides with an existing slug, so the caller's input keeps
// looking human in /settings/team.
func (s *Service) Create(ctx context.Context, request appmodel.TeamCreateRequest) (Team, error) {
	if request.OwnerID <= 0 {
		return Team{}, ErrNotFound
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return Team{}, fmt.Errorf("%w: name required", ErrValidation)
	}
	base := Slugify(request.Name)
	request.Slug = base
	for i := 2; ; i++ {
		id, err := s.teams.CreateOwnedTeam(ctx, request)
		if err == nil {
			return s.FindByID(ctx, id)
		}
		if !errors.Is(err, model.ErrAlreadyExists) {
			return Team{}, fmt.Errorf("insert team: %w", err)
		}
		request.Slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// FindByID looks up a team by primary key.
func (s *Service) FindByID(ctx context.Context, id int64) (Team, error) {
	if id <= 0 {
		return Team{}, ErrNotFound
	}
	team, err := s.teams.FindTeam(ctx, id)
	return team, mapStoreError(err)
}

// FindByIDs loads workspace names for a set of IDs in bounded batch queries.
func (s *Service) FindByIDs(ctx context.Context, ids []int64) (map[int64]Team, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrNotFound
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[int64]Team{}, nil
	}
	teams, err := s.teams.FindTeams(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("find workspaces: %w", err)
	}
	return teams, nil
}

// FindBySlug looks up a team by its URL slug.
func (s *Service) FindBySlug(ctx context.Context, slug string) (Team, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return Team{}, ErrNotFound
	}
	team, err := s.teams.FindTeamBySlug(ctx, slug)
	return team, mapStoreError(err)
}

// ListForUser returns every team the user is a member of. Owner rows
// appear first; within each role bucket results are sorted by joined_at.
func (s *Service) ListForUser(ctx context.Context, userID int64) ([]Team, error) {
	if userID <= 0 {
		return nil, ErrNotFound
	}
	return s.teams.ListTeamsForUser(ctx, userID)
}

// MembershipsForUser returns workspaces with each role in one persistence
// call, for navigation and authorization-aware presentation.
func (s *Service) MembershipsForUser(ctx context.Context, userID int64) ([]model.TeamMembership, error) {
	if userID <= 0 {
		return nil, ErrNotFound
	}
	return s.memberships.ListMembershipsForUser(ctx, userID)
}

// MembershipForUser resolves a workspace and the caller's role together.
func (s *Service) MembershipForUser(ctx context.Context, query appmodel.TeamMembershipQuery) (model.TeamMembership, bool, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return model.TeamMembership{}, false, ErrNotFound
	}
	return s.memberships.FindMembershipForUser(ctx, query)
}

// Rename changes the team's display name. Slug stays the same to keep
// URLs stable; only the human-facing name updates.
func (s *Service) Rename(ctx context.Context, request appmodel.TeamRenameRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return fmt.Errorf("%w: name required", ErrValidation)
	}
	return mapStoreError(s.teams.RenameTeam(ctx, request))
}

// UpdateBilling validates and stores the team's time-rounding and invoice
// numbering preferences. The settings are shared by every adapter that
// creates billable documents.
// Delete removes the team and — via ON DELETE CASCADE — every activity,
// session, tag, goal, membership and invite that belongs to it. Only
// the owner can do this; if the caller isn't the owner, ErrForbidden is
// returned. Teams with more than one member can't be deleted (the
// caller has to remove the others first) to avoid orphaning anyone.
func (s *Service) Delete(ctx context.Context, request appmodel.WorkspaceDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	_, err := s.DeleteAndListRemaining(ctx, request)
	return err
}

// DeleteAndListRemaining removes an owned single-member workspace and returns
// the caller's remaining workspaces from the same persistence transaction.
func (s *Service) DeleteAndListRemaining(ctx context.Context, request appmodel.WorkspaceDeleteRequest) ([]Team, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return nil, ErrNotFound
	}
	remaining, err := s.teams.DeleteTeamAndListRemaining(ctx, request)
	if err != nil {
		if errors.Is(err, model.ErrTeamHasMembers) {
			return nil, fmt.Errorf("%w: remove all members first", ErrValidation)
		}
		return nil, mapStoreError(err)
	}
	return remaining, nil
}

// IsMember reports whether userID is a member of teamID. Cheap — one
// indexed lookup. Returns the role too so the caller can authorise.
func mapStoreError(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, model.ErrForbidden) {
		return ErrForbidden
	}
	return err
}

func mapStoreForbidden(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return ErrForbidden
	}
	return err
}
