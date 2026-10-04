package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

type teamModulesReaderStub struct {
	TeamSettingsManagement
	calls   int
	enabled map[string]bool
	err     error
}

func (stub *teamModulesReaderStub) SectionModules(context.Context, int64) (map[string]bool, error) {
	stub.calls++
	return stub.enabled, stub.err
}

func TestTeamModulesLoadsOncePerRequestAndReturnsCopies(t *testing.T) {
	settings := &teamModulesReaderStub{enabled: map[string]bool{"reports": true}}
	server := &Server{services: Dependencies{Teams: TeamDependencies{Settings: settings}}}
	ctx := WithTeam(context.Background(), model.Team{ID: 9})
	ctx = context.WithValue(ctx, ctxTeamModulesKey, &teamModulesCache{})
	request := httptest.NewRequest("GET", "/", nil).WithContext(ctx)

	first := server.teamModules(request)
	first["reports"] = false
	second := server.teamModules(request)
	server.userModules(request)

	if settings.calls != 1 {
		t.Fatalf("SectionModules calls = %d, want one per request", settings.calls)
	}
	if !second["reports"] {
		t.Fatal("mutating a returned module map changed the cached settings")
	}
}

func TestTeamModulesCacheFailsClosedAndReadsFailureOnce(t *testing.T) {
	settings := &teamModulesReaderStub{enabled: map[string]bool{"reports": true}, err: errors.New("settings unavailable")}
	server := &Server{services: Dependencies{Teams: TeamDependencies{Settings: settings}}}
	ctx := WithTeam(context.Background(), model.Team{ID: 9})
	ctx = context.WithValue(ctx, ctxTeamModulesKey, &teamModulesCache{})
	request := httptest.NewRequest("GET", "/", nil).WithContext(ctx)

	for range 2 {
		if enabled := server.teamModules(request); len(enabled) != 0 {
			t.Fatalf("failed settings read enabled modules: %v", enabled)
		}
	}
	if settings.calls != 1 {
		t.Fatalf("SectionModules calls after cached failure = %d, want one", settings.calls)
	}
}
