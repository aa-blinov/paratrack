package projects

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/translit"
)

var ErrRebindForbidden = model.ErrProjectRebindForbidden

// AssignActivity enforces member and workspace rules before the scoped
// persistence operation attaches an activity to a project.
func (s *Service) AssignActivity(ctx context.Context, request appmodel.AssignActivityProjectRequest) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	if request.ActivityID <= 0 || request.ProjectID < 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	role, member, err := s.authorization.TeamMemberRole(ctx, appmodel.TeamMembershipQuery{TeamID: request.TeamID, UserID: request.CallerID})
	if err != nil {
		return fmt.Errorf("resolve project assignment role: %w", err)
	}
	if !member {
		return model.ErrForbidden
	}
	if role.CanManage() {
		return s.writes.AssignActivityProject(ctx, request)
	}
	activity, err := s.catalog.GetActivity(ctx, appmodel.ActivityLookupQuery{TeamID: request.TeamID, ActivityID: request.ActivityID})
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load activity before assigning project: %w", err)
	}
	if activity.ProjectID == request.ProjectID {
		return nil
	}
	if activity.ProjectID != 0 || request.ProjectID == 0 {
		return ErrRebindForbidden
	}
	if err := s.writes.AssignFirstActivityProject(ctx, request); err != nil {
		if errors.Is(err, model.ErrForbidden) {
			return ErrRebindForbidden
		}
		return err
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.ProjectMutationRequest) error {
	if request.TeamID <= 0 {
		return fmt.Errorf("invalid project delete request")
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return fmt.Errorf("invalid project delete request")
	}
	return s.writes.DeleteProject(ctx, request)
}

// DeleteBySlug resolves the route identity in the project workflow before
// applying the scoped deletion command.
func (s *Service) DeleteBySlug(ctx context.Context, request appmodel.ProjectSlugMutationRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || strings.TrimSpace(request.Slug) == "" {
		return model.ErrNotFound
	}
	project, err := s.GetBySlug(ctx, appmodel.ProjectSlugQuery{TeamID: request.TeamID, Slug: request.Slug})
	if err != nil {
		return err
	}
	return s.Delete(ctx, appmodel.ProjectMutationRequest{
		TeamID: request.TeamID, ProjectID: project.ID, CallerID: request.CallerID,
	})
}

var (
	ErrDuplicate       = model.ErrAlreadyExists
	ErrNotFound        = model.ErrNotFound
	ErrAmbiguousSlug   = model.ErrAmbiguousProject
	ErrAlreadyBilled   = model.ErrAlreadyBilled
	ErrInvalidTeam     = appmodel.ErrInvalidProjectTeam
	ErrInvalidRate     = appmodel.ErrInvalidProjectRate
	ErrInvalidCurrency = appmodel.ErrInvalidProjectCurrency
	ErrInvalidEstimate = appmodel.ErrInvalidProjectEstimate
	ErrInvalidName     = appmodel.ErrInvalidProjectName
	ErrInvalidSlug     = appmodel.ErrInvalidProjectSlug
	ErrInvalidColor    = appmodel.ErrInvalidProjectColor
)

var projectSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var projectColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeProjectInput(name, slug, color string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", ErrInvalidName
	}
	if strings.TrimSpace(slug) == "" {
		slug = slugify(name)
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !projectSlugPattern.MatchString(slug) {
		return "", "", "", ErrInvalidSlug
	}
	if color == "" {
		color = "#7c8499"
	}
	if !projectColorPattern.MatchString(color) {
		return "", "", "", ErrInvalidColor
	}
	return name, slug, color, nil
}

func slugify(name string) string {
	var b strings.Builder
	previousDash := false
	for _, r := range translit.Latin(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			previousDash = false
		} else if !previousDash && b.Len() > 0 {
			b.WriteByte('-')
			previousDash = true
		}
	}
	if slug := strings.TrimRight(b.String(), "-"); slug != "" {
		return slug
	}
	return "project"
}

// Create validates billing defaults and persists the project atomically.
func (s *Service) Create(ctx context.Context, request appmodel.ProjectCreateRequest) (model.Project, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	var err error
	request.Name, request.Slug, request.Color, err = normalizeProjectInput(request.Name, request.Slug, request.Color)
	if err != nil {
		return model.Project{}, err
	}
	if request.RateCents != nil && *request.RateCents < 0 {
		return model.Project{}, ErrInvalidRate
	}
	request.Currency = strings.TrimSpace(request.Currency)
	if request.Currency != "" && (len(request.Currency) != 3 || strings.ToUpper(request.Currency) != request.Currency) {
		return model.Project{}, ErrInvalidCurrency
	}
	project, err := s.writes.CreateProjectWithBilling(ctx, request)
	if err != nil {
		if errors.Is(err, model.ErrAlreadyExists) {
			return model.Project{}, ErrDuplicate
		}
		return model.Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

// Update applies a project edit, including optional billing settings, as one
// persistence operation.
func (s *Service) Update(ctx context.Context, request appmodel.ProjectUpdateRequest) (model.Project, error) {
	if request.TeamID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return model.Project{}, model.ErrNotFound
	}
	if request.Update.Name != "" {
		request.Update.Name = strings.TrimSpace(request.Update.Name)
		if request.Update.Name == "" {
			return model.Project{}, ErrInvalidName
		}
	}
	if request.Update.Color != "" && !projectColorPattern.MatchString(request.Update.Color) {
		return model.Project{}, ErrInvalidColor
	}
	if request.Update.RateCents != nil && *request.Update.RateCents < 0 {
		return model.Project{}, ErrInvalidRate
	}
	if request.Update.EstimateMinutes != nil && *request.Update.EstimateMinutes < 0 {
		return model.Project{}, ErrInvalidEstimate
	}
	if request.Update.Currency != nil {
		currency := strings.TrimSpace(*request.Update.Currency)
		if currency != "" && (len(currency) != 3 || strings.ToUpper(currency) != currency) {
			return model.Project{}, ErrInvalidCurrency
		}
		request.Update.Currency = &currency
	}
	project, err := s.writes.UpdateProjectWithOptions(ctx, request)
	if err != nil {
		return model.Project{}, fmt.Errorf("update project: %w", err)
	}
	return project, nil
}

// UpdateBySlug resolves the route identity in the project workflow before
// applying the scoped update command.
func (s *Service) UpdateBySlug(ctx context.Context, request appmodel.ProjectSlugUpdateRequest) (model.Project, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || strings.TrimSpace(request.Slug) == "" {
		return model.Project{}, model.ErrNotFound
	}
	project, err := s.GetBySlug(ctx, appmodel.ProjectSlugQuery{TeamID: request.TeamID, Slug: request.Slug})
	if err != nil {
		return model.Project{}, err
	}
	return s.Update(ctx, appmodel.ProjectUpdateRequest{
		TeamID: request.TeamID, ProjectID: project.ID, CallerID: request.CallerID, Update: request.Update,
	})
}

// UpdateRate changes the billable rate and flag together while preserving any
// field omitted by the adapter.
func (s *Service) UpdateRate(ctx context.Context, request appmodel.ProjectRateRequest) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	if request.ProjectID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if request.RateCents != nil && *request.RateCents < 0 {
		return ErrInvalidRate
	}
	if request.RateCents == nil && request.Billable == nil {
		return nil
	}
	if err := s.writes.SetProjectRate(ctx, request); err != nil {
		return fmt.Errorf("update project rate: %w", err)
	}
	return nil
}
