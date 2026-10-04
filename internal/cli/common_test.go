package cli

import (
	"context"
	"testing"

	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type defaultTeamWorkspaceStub struct{}
type defaultTeamProjectLookupStub struct{ cliport.ProjectLookup }
type defaultTeamProjectStub struct{ cliport.Projects }

func (defaultTeamWorkspaceStub) DefaultTeam(context.Context) (int64, error) {
	return 17, nil
}

func (defaultTeamWorkspaceStub) TeamOwnerID(context.Context, int64) (int64, error) {
	return 29, nil
}

func TestDefaultTeamForCommandSetsWorkspaceOwner(t *testing.T) {
	services := &cliport.Services{
		Workspace:     defaultTeamWorkspaceStub{},
		ProjectLookup: defaultTeamProjectLookupStub{},
		Projects:      defaultTeamProjectStub{},
	}
	teamID, ctx, err := defaultTeamForCommand(services, context.Background())
	if err != nil {
		t.Fatalf("resolve default team: %v", err)
	}
	if teamID != 17 {
		t.Fatalf("team ID = %d, want 17", teamID)
	}
	if actorID := requestctx.ActorID(ctx); actorID != 29 {
		t.Fatalf("actor ID = %d, want workspace owner 29", actorID)
	}
	if scopedUserID := requestctx.ScopedUserID(ctx); scopedUserID != 29 {
		t.Fatalf("session scope = %d, want workspace owner 29", scopedUserID)
	}
}
