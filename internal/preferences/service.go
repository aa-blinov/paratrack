// Package preferences owns personal display preferences and workspace-scoped
// defaults that are stored on a user account.
package preferences

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type Prefs = appmodel.UserPreferences

// persistedPrefs owns the stable JSON representation stored in the user row.
// It is intentionally separate from the application contract and HTTP DTOs.
type persistedPrefs struct {
	HiddenSections []string         `json:"hidden,omitempty"`
	Tabs           []string         `json:"tabs,omitempty"`
	Duration       string           `json:"duration,omitempty"`
	WeekStart      string           `json:"week_start,omitempty"`
	TZ             string           `json:"tz,omitempty"`
	HiddenWidgets  []string         `json:"widgets_hidden,omitempty"`
	DefaultProject map[string]int64 `json:"default_project,omitempty"`
}

func (p persistedPrefs) preferences() Prefs {
	return Prefs{
		HiddenSections: p.HiddenSections, Tabs: p.Tabs, Duration: p.Duration,
		WeekStart: p.WeekStart, TZ: p.TZ, HiddenWidgets: p.HiddenWidgets,
		DefaultProject: p.DefaultProject,
	}
}

func persisted(prefs Prefs) persistedPrefs {
	return persistedPrefs{
		HiddenSections: prefs.HiddenSections, Tabs: prefs.Tabs, Duration: prefs.Duration,
		WeekStart: prefs.WeekStart, TZ: prefs.TZ, HiddenWidgets: prefs.HiddenWidgets,
		DefaultProject: prefs.DefaultProject,
	}
}

type Store interface {
	UserPrefs(context.Context, int64) (string, error)
	SetUserPrefs(context.Context, appmodel.UserPrefsSaveCommand) error
}

type ProjectLookup interface {
	GetInTeam(context.Context, int64, int64) (model.Project, error)
}

var ErrIncompleteDependencies = errors.New("preference service dependencies are incomplete")

type Service struct {
	store    Store
	projects ProjectLookup
}

func New(store Store, projects ProjectLookup) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, fmt.Errorf("%w: store", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(projects) {
		return nil, fmt.Errorf("%w: project lookup", ErrIncompleteDependencies)
	}
	return &Service{store: store, projects: projects}, nil
}

var (
	ErrInvalidDefaultProject = appmodel.ErrInvalidDefaultProject
	ErrInvalidPreferences    = appmodel.ErrInvalidPreferences
)

func (s *Service) Load(ctx context.Context, userID int64) (Prefs, error) {
	if userID <= 0 {
		return Prefs{}, model.ErrNotFound
	}
	raw, err := s.store.UserPrefs(ctx, userID)
	if err != nil {
		return Prefs{}, fmt.Errorf("load user preferences: %w", err)
	}
	var stored persistedPrefs
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return Prefs{}, fmt.Errorf("decode user preferences: %w", err)
		}
	}
	return stored.preferences(), nil
}

func (s *Service) Save(ctx context.Context, request appmodel.PreferencesSaveRequest) error {
	if request.UserID <= 0 || request.CallerID <= 0 || request.TeamID <= 0 {
		return model.ErrNotFound
	}
	if request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	userID, teamID, prefs := request.UserID, request.TeamID, request.Preferences
	if projectID := prefs.DefaultProject[fmt.Sprint(teamID)]; projectID > 0 {
		project, err := s.projects.GetInTeam(ctx, teamID, projectID)
		if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrForbidden) {
			return ErrInvalidDefaultProject
		}
		if err != nil {
			return fmt.Errorf("validate default project: %w", err)
		}
		if project.TeamID != teamID {
			return ErrInvalidDefaultProject
		}
	}
	raw, err := json.Marshal(persisted(prefs))
	if err != nil {
		return fmt.Errorf("encode user preferences: %w", err)
	}
	if len(raw) > 16*1024 {
		return ErrInvalidPreferences
	}
	if err := s.store.SetUserPrefs(ctx, appmodel.UserPrefsSaveCommand{UserID: userID, CallerID: request.CallerID, JSON: string(raw)}); err != nil {
		return fmt.Errorf("save user preferences: %w", err)
	}
	return nil
}
