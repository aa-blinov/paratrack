package teams

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func DecodeSections(stored string) map[string]bool { return appmodel.DecodeSections(stored) }

func (s *Service) SectionModules(ctx context.Context, teamID int64) (map[string]bool, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	stored, err := s.modules.TeamModules(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load team modules: %w", err)
	}
	return DecodeSections(stored), nil
}

func (s *Service) UpdateModules(ctx context.Context, request appmodel.TeamModulesRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	if err := s.modules.SetTeamModules(ctx, request); err != nil {
		return fmt.Errorf("update team modules: %w", err)
	}
	return nil
}
