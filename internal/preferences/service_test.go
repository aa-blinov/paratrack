package preferences

import (
	"context"
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

type preferenceStoreStub struct {
	stored   string
	saveCall int
}

func (s *preferenceStoreStub) UserPrefs(context.Context, int64) (string, error) {
	return s.stored, nil
}

func (s *preferenceStoreStub) SetUserPrefs(_ context.Context, request appmodel.UserPrefsSaveCommand) error {
	s.saveCall++
	s.stored = request.JSON
	return nil
}

type projectLookupStub struct {
	teamID    int64
	projectID int64
	project   model.Project
	err       error
	calls     int
}

func (s *projectLookupStub) GetInTeam(_ context.Context, query appmodel.ProjectScopeQuery) (model.Project, error) {
	s.calls++
	s.teamID, s.projectID = query.TeamID, query.ProjectID
	return s.project, s.err
}

func TestSaveRejectsDefaultProjectFromAnotherWorkspace(t *testing.T) {
	store := &preferenceStoreStub{}
	projects := &projectLookupStub{project: model.Project{ID: 42, TeamID: 9}}
	service, err := New(store, projects)
	if err != nil {
		t.Fatal(err)
	}

	err = service.Save(context.Background(), appmodel.PreferencesSaveRequest{UserID: 5, CallerID: 5, TeamID: 7, Preferences: Prefs{
		DefaultProject: map[string]int64{"7": 42},
	}})
	if !errors.Is(err, ErrInvalidDefaultProject) {
		t.Fatalf("Save error = %v, want %v", err, ErrInvalidDefaultProject)
	}
	if projects.calls != 1 || projects.teamID != 7 || projects.projectID != 42 {
		t.Fatalf("project lookup = (%d, %d, calls %d), want (7, 42, 1)", projects.teamID, projects.projectID, projects.calls)
	}
	if store.saveCall != 0 {
		t.Fatalf("preference store writes = %d, want 0", store.saveCall)
	}
}

func TestSaveRejectsPreferencesForAnotherUser(t *testing.T) {
	store := &preferenceStoreStub{}
	service, err := New(store, &projectLookupStub{})
	if err != nil {
		t.Fatal(err)
	}
	err = service.Save(context.Background(), appmodel.PreferencesSaveRequest{UserID: 10, CallerID: 5, TeamID: 7})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("Save error = %v, want request rejected", err)
	}
	if store.saveCall != 0 {
		t.Fatalf("preference store writes = %d, want 0", store.saveCall)
	}
}

func TestSaveIgnoresDefaultsForOtherWorkspaces(t *testing.T) {
	store := &preferenceStoreStub{}
	projects := &projectLookupStub{}
	service, err := New(store, projects)
	if err != nil {
		t.Fatal(err)
	}

	err = service.Save(context.Background(), appmodel.PreferencesSaveRequest{UserID: 5, CallerID: 5, TeamID: 7, Preferences: Prefs{
		DefaultProject: map[string]int64{"9": 42},
	}})
	if err != nil {
		t.Fatalf("Save error = %v", err)
	}
	if projects.calls != 0 {
		t.Fatalf("project lookups = %d, want 0", projects.calls)
	}
	if store.saveCall != 1 {
		t.Fatalf("preference store writes = %d, want 1", store.saveCall)
	}
}

func TestPreferencesKeepPersistedJSONShape(t *testing.T) {
	store := &preferenceStoreStub{}
	service, err := New(store, &projectLookupStub{project: model.Project{ID: 42, TeamID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	want := Prefs{
		HiddenSections: []string{"reports"},
		WeekStart:      "monday",
		DefaultProject: map[string]int64{"7": 42},
	}
	if err := service.Save(context.Background(), appmodel.PreferencesSaveRequest{UserID: 5, CallerID: 5, TeamID: 7, Preferences: want}); err != nil {
		t.Fatalf("Save error = %v", err)
	}
	if store.stored != `{"hidden":["reports"],"week_start":"monday","default_project":{"7":42}}` {
		t.Fatalf("stored preferences = %s", store.stored)
	}
	got, err := service.Load(context.Background(), 5)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if len(got.HiddenSections) != 1 || got.HiddenSections[0] != "reports" || got.WeekStart != "monday" || got.DefaultProject["7"] != 42 {
		t.Fatalf("loaded preferences = %#v", got)
	}
}

func TestSaveRejectsOversizedPreferencesBeforeWriting(t *testing.T) {
	store := &preferenceStoreStub{}
	service, err := New(store, &projectLookupStub{})
	if err != nil {
		t.Fatal(err)
	}

	err = service.Save(context.Background(), appmodel.PreferencesSaveRequest{UserID: 5, CallerID: 5, TeamID: 7, Preferences: Prefs{
		HiddenSections: []string{strings.Repeat("x", 16*1024)},
	}})
	if !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("Save error = %v, want %v", err, ErrInvalidPreferences)
	}
	if store.saveCall != 0 {
		t.Fatalf("preference store writes = %d, want 0", store.saveCall)
	}
}
