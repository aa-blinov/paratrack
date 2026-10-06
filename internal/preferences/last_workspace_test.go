package preferences

import (
	"context"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// TestTheWorkspaceSomeoneWorkedInLastSurvivesASave is the round trip the
// login path depends on: the remembered workspace has to come back out
// of the stored blob, next to the preferences that were already there.
func TestTheWorkspaceSomeoneWorkedInLastSurvivesASave(t *testing.T) {
	store := &preferenceStoreStub{}
	service, err := New(store, &projectLookupStub{})
	if err != nil {
		t.Fatal(err)
	}

	want := Prefs{Duration: "hm", WeekStart: "sun", TZ: "Europe/Moscow", LastTeamID: 9}
	if err := service.Save(context.Background(), appmodel.PreferencesSaveRequest{
		UserID: 5, CallerID: 5, TeamID: 9, Preferences: want,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.Contains(store.stored, `"last_team":9`) {
		t.Errorf("stored blob should carry the remembered workspace, got %s", store.stored)
	}

	got, err := service.Load(context.Background(), 5)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastTeamID != 9 {
		t.Errorf("LastTeamID = %d, want 9", got.LastTeamID)
	}
	if got.Duration != want.Duration || got.WeekStart != want.WeekStart || got.TZ != want.TZ {
		t.Errorf("preferences around it changed: %+v", got)
	}
}

// TestAnAccountWithNoChosenWorkspaceStoresNoWorkspace keeps a fresh
// account's blob free of a zero id, so a later login has nothing to
// guess from and lands where it always did.
func TestAnAccountWithNoChosenWorkspaceStoresNoWorkspace(t *testing.T) {
	store := &preferenceStoreStub{}
	service, err := New(store, &projectLookupStub{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Save(context.Background(), appmodel.PreferencesSaveRequest{
		UserID: 5, CallerID: 5, TeamID: 4, Preferences: Prefs{},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if strings.Contains(store.stored, "last_team") {
		t.Errorf("nothing was chosen, so the blob should omit the workspace: %s", store.stored)
	}
	got, err := service.Load(context.Background(), 5)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastTeamID != 0 {
		t.Errorf("LastTeamID = %d, want 0", got.LastTeamID)
	}
}
