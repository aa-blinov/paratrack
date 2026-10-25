// Package search answers the global search: one text across a workspace's clients,
// projects, tasks, tags, people and sessions (UI-01).
package search

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
)

// Store finds the matches in one workspace, as the viewer may see them.
type Store interface {
	SearchWorkspace(context.Context, appmodel.SearchQuery) (appmodel.SearchResults, error)
}

// Dependencies are what the search reads from.
type Dependencies struct {
	Store Store
}

// Service answers the global search.
type Service struct {
	store Store
}

var ErrInvalidSearch = errors.New("invalid search")

// defaultHits caps each group of the global search, so a short text cannot pull the
// whole workspace. The CRM search box asks for more, up to maxHits.
const (
	defaultHits = 8
	maxHits     = 50
)

// New builds the search service.
func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Store) {
		return nil, errors.New("search store is nil")
	}
	return &Service{store: deps.Store}, nil
}

// Find answers a text with the matches the viewer may see. A text shorter than two
// characters finds nothing, so a single letter does not list the workspace.
func (s *Service) Find(ctx context.Context, query appmodel.SearchQuery) (appmodel.SearchResults, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return appmodel.SearchResults{}, ErrInvalidSearch
	}
	query.Text = strings.TrimSpace(query.Text)
	if utf8.RuneCountInString(query.Text) < 2 {
		return appmodel.SearchResults{}, nil
	}
	switch {
	case query.Limit <= 0:
		query.Limit = defaultHits
	case query.Limit > maxHits:
		query.Limit = maxHits
	}
	return s.store.SearchWorkspace(ctx, query)
}
