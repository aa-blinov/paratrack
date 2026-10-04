// Package tokenadmin assembles the read model shown on the API token settings page.
package tokenadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type TokenReader interface {
	ListAPITokens(context.Context, appmodel.APITokenListRequest) ([]appmodel.APITokenSummary, error)
}

type TeamReader interface {
	FindByIDs(context.Context, []int64) (map[int64]model.Team, error)
}

type Dependencies struct {
	Tokens TokenReader
	Teams  TeamReader
}

var ErrIncompleteDependencies = errors.New("API token management dependencies are incomplete")

type Builder struct {
	tokens TokenReader
	teams  TeamReader
}

func New(deps Dependencies) (*Builder, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"token reader", deps.Tokens}, {"team reader", deps.Teams},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{tokens: deps.Tokens, teams: deps.Teams}, nil
}

func (b *Builder) Management(ctx context.Context, request appmodel.APITokenListRequest) (appmodel.APITokenManagementSnapshot, error) {
	if request.UserID <= 0 || request.CallerID <= 0 {
		return appmodel.APITokenManagementSnapshot{}, model.ErrNotFound
	}
	tokens, err := b.tokens.ListAPITokens(ctx, request)
	if err != nil {
		return appmodel.APITokenManagementSnapshot{}, fmt.Errorf("list API tokens: %w", err)
	}
	teamIDs := distinctTeamIDs(tokens)
	teamNames := make(map[int64]string, len(teamIDs))
	if len(teamIDs) > 0 {
		teams, err := b.teams.FindByIDs(ctx, teamIDs)
		if err != nil {
			return appmodel.APITokenManagementSnapshot{}, fmt.Errorf("load API token team names: %w", err)
		}
		for id, team := range teams {
			teamNames[id] = team.Name
		}
	}
	return appmodel.APITokenManagementSnapshot{Tokens: tokens, TeamNames: teamNames}, nil
}

func distinctTeamIDs(tokens []appmodel.APITokenSummary) []int64 {
	seen := make(map[int64]struct{}, len(tokens))
	ids := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		id := token.TeamID
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
