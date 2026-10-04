package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestWorkspaceListQueriesRejectMissingTeamScope(t *testing.T) {
	database := &DB{}

	if _, err := database.ListActivities(t.Context(), 0, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListActivities with no workspace = %v, want ErrNotFound", err)
	}
	if _, err := database.ListGoals(t.Context(), appmodel.GoalListQuery{TeamID: 0}); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListGoals with no workspace = %v, want ErrNotFound", err)
	}
	if _, err := database.ListSavedReports(t.Context(), 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListSavedReports with no workspace = %v, want ErrNotFound", err)
	}
}
