package search

import (
	"context"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type countingStore struct{ calls int }

func (s *countingStore) SearchWorkspace(context.Context, appmodel.SearchQuery) (appmodel.SearchResults, error) {
	s.calls++
	return appmodel.SearchResults{}, nil
}

// A single letter finds nothing and does not reach the store, so typing one key does not
// list the whole workspace (UI-01).
func TestSearchNeedsTwoCharactersAndCapsEachGroup(t *testing.T) {
	store := &countingStore{}
	service, err := New(Dependencies{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Find(context.Background(), appmodel.SearchQuery{TeamID: 1, UserID: 1, Text: " a "}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatalf("a one-letter search reached the store %d times", store.calls)
	}
	if _, err := service.Find(context.Background(), appmodel.SearchQuery{TeamID: 0, UserID: 1, Text: "сайт"}); err == nil {
		t.Fatal("a search without a workspace was accepted")
	}
}
