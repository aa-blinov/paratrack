package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestCreateProject_Defaults(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	// A team is required by the FK. Each test schema starts without seed
	// rows, so create the team explicitly.
	teamID := seedTeam(t, d, "Test Team", "tt")

	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "EORA RAG", Slug: "", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "EORA RAG" {
		t.Errorf("expected name %q, got %q", "EORA RAG", p.Name)
	}
	if p.Slug != "eora-rag" {
		t.Errorf("expected slug %q (auto), got %q", "eora-rag", p.Slug)
	}
	if p.Color != DefaultProjectColor {
		t.Errorf("expected default color %q, got %q", DefaultProjectColor, p.Color)
	}
	if p.Archived {
		t.Errorf("new project should not be archived")
	}
	if p.TeamID != teamID {
		t.Errorf("expected teamID=%d, got %d", teamID, p.TeamID)
	}
}

func TestCreateProject_CustomSlugAndColor(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "Side Project", Slug: "side", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "side" {
		t.Errorf("expected explicit slug 'side', got %q", p.Slug)
	}
	if p.Color != "#7c3aed" {
		t.Errorf("expected color #7c3aed, got %q", p.Color)
	}
}

func TestCreateProject_DuplicateSlug(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	if _, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "First", Slug: "shared", Color: ""}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "Second", Slug: "shared", Color: ""})
	if err != ErrDuplicate {
		t.Errorf("expected ErrDuplicate on slug collision, got %v", err)
	}
}

func TestCreateProject_RejectsBadColor(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	for _, bad := range []string{"red", "#abc", "#abcd", "7c3aed"} {
		if _, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "x", Slug: "y", Color: bad}); err == nil {
			t.Errorf("expected error for bad color %q", bad)
		}
	}
}

func TestGetProjectBySlug(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	created, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "EORA RAG", Slug: "", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.GetProjectBySlug(ctx, appmodel.ProjectSlugQuery{TeamID: teamID, Slug: "EORA-RAG"}) // case-insensitive
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("lookup id mismatch: %d vs %d", got.ID, created.ID)
	}
}

func TestListProjects_HidesArchived(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	a, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "Alpha", Slug: "", Color: ""})
	b, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "Bravo", Slug: "", Color: ""})
	if _, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamID, ProjectID: a.ID, CallerID: teamOwner(t, d, teamID), Update: appmodel.ProjectUpdate{Name: "", Color: "", Archived: boolPtr(true), EstimateMinutes: nil}}); err != nil {
		t.Fatal(err)
	}

	// Default: archived hidden.
	list, err := d.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("expected only Bravo, got %d entries", len(list))
	}

	// includeArchived: both visible, archived last.
	all, err := d.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID, IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	if all[0].ID != b.ID {
		t.Errorf("expected active project first (Bravo=%d), got %d", b.ID, all[0].ID)
	}
	if !all[1].Archived {
		t.Errorf("expected archived project last")
	}
}

func TestUpdateProject_Partial(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "EORA RAG", Slug: "eora", Color: "#aaaaaa"})

	// Only color.
	upd, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID), Update: appmodel.ProjectUpdate{Name: "", Color: "#bbbbbb", Archived: nil, EstimateMinutes: nil}})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "EORA RAG" {
		t.Errorf("name should be unchanged, got %q", upd.Name)
	}
	if upd.Color != "#bbbbbb" {
		t.Errorf("color should be updated, got %q", upd.Color)
	}

	// Only name.
	upd, err = d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID), Update: appmodel.ProjectUpdate{Name: "EORA v2", Color: "", Archived: nil, EstimateMinutes: nil}})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "EORA v2" || upd.Color != "#bbbbbb" {
		t.Errorf("partial update wrong: %+v", upd)
	}

	// Only archived.
	upd, err = d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID), Update: appmodel.ProjectUpdate{Name: "", Color: "", Archived: boolPtr(true), EstimateMinutes: nil}})
	if err != nil {
		t.Fatal(err)
	}
	if !upd.Archived {
		t.Error("expected archived=true")
	}
	if upd.Color != "#bbbbbb" {
		t.Errorf("color should persist across partial updates, got %q", upd.Color)
	}
}

func TestUpdateProject_CrossTeamForbidden(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "Team A", "a")
	teamB := seedTeam(t, d, "Team B", "b")
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamA, CallerID: teamOwner(t, d, teamA), Name: "Owned by A", Slug: "owned-a", Color: ""})

	// Team B trying to rename should fail (not see it as theirs).
	_, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamB, ProjectID: p.ID, CallerID: teamOwner(t, d, teamB), Update: appmodel.ProjectUpdate{Name: "Hacked", Color: "", Archived: nil, EstimateMinutes: nil}})
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound on cross-team update, got %v", err)
	}
}

func TestDeleteProject_PreservesActivities(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "X", Slug: "x", Color: ""})
	a, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "writing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: a.ID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID)}); err != nil {
		t.Fatal(err)
	}

	// Delete the project; the activity should survive with project_id=NULL.
	if err := d.DeleteProject(ctx, appmodel.ProjectMutationRequest{TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID)}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetActivity(ctx, teamID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != 0 {
		t.Errorf("activity project_id should be 0 after project delete, got %d", got.ProjectID)
	}
}

func TestDeleteProject_WrongTeamIsNotFound(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "A", "a")
	teamB := seedTeam(t, d, "B", "b")
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamA, CallerID: teamOwner(t, d, teamA), Name: "x", Slug: "x", Color: ""})

	if err := d.DeleteProject(ctx, appmodel.ProjectMutationRequest{TeamID: teamB, ProjectID: p.ID, CallerID: teamOwner(t, d, teamB)}); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAssignActivityProject_CrossTeamForbidden(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "A", "a")
	teamB := seedTeam(t, d, "B", "b")
	// Project lives in A.
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamA, CallerID: teamOwner(t, d, teamA), Name: "x", Slug: "x", Color: ""})
	// Activity lives in B.
	_, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamB, CallerID: teamOwner(t, d, teamB), Name: "writing"})
	if err != nil {
		t.Fatal(err)
	}
	aInB, err := d.getActivityByName(ctx, teamB, "writing")
	if err != nil {
		t.Fatal(err)
	}

	// Team B tries to attach A's project to its activity — must fail.
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamB, ActivityID: aInB.ID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamB)}); err != ErrNotFound {
		t.Errorf("expected ErrNotFound on cross-team assign, got %v", err)
	}

	// Same team, valid assign.
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamB, ActivityID: aInB.ID, CallerID: teamOwner(t, d, teamB)}); err != nil {
		// 0 means clear; not a cross-team case.
		if err != ErrNotFound {
			t.Errorf("clearing project should succeed, got %v", err)
		}
	}
}

func TestListActivitiesForProject(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "P", Slug: "p", Color: ""})

	a1, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "writing"})
	a2, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "reading"})
	// Activity without project.
	_, _ = d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "unrelated"})
	_ = d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: a1.ID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID)})
	_ = d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: a2.ID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID)})

	list, err := d.ListActivitiesForProject(ctx, teamID, p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 activities in project, got %d", len(list))
	}
	// Sorted by name: reading < writing
	if list[0].Name != "reading" || list[1].Name != "writing" {
		t.Errorf("expected alphabetical order, got %s, %s", list[0].Name, list[1].Name)
	}
}

func TestSlugsLowercaseOnly(t *testing.T) {
	for _, s := range []string{"EORA RAG v2", "  spaces  ", "Mixed_CASE", "  ", "café"} {
		got := slugify(s)
		if strings.ToLower(got) != got {
			t.Errorf("slugify(%q) = %q, should be lowercase", s, got)
		}
	}
	if slugify("") != "project" {
		t.Errorf("empty input should fall back to 'project'")
	}
}

func TestAssignActivityProject_RechecksManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "P", Slug: "p", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: project.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("expected current member role to be rechecked, got %v", err)
	}
	got, err := d.GetActivity(ctx, teamID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != 0 {
		t.Fatalf("unauthorized assignment changed project to %d", got.ProjectID)
	}
}

func TestAssignFirstActivityProject_RequiresCurrentMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "P", Slug: "p", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	outsiderID := seedProjectTestUser(t, d, "outsider")

	if err := d.AssignFirstActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: project.ID, CallerID: outsiderID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("expected current membership check to reject outsider, got %v", err)
	}
	got, err := d.GetActivity(ctx, teamID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != 0 {
		t.Fatalf("unauthorized assignment changed project to %d", got.ProjectID)
	}
}

func TestProjectWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "P", Slug: "p", Color: ""})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: memberID, Name: "Unauthorized", Slug: "unauthorized", Color: ""}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("CreateProject error = %v, want forbidden", err)
	}
	if _, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: teamID, ProjectID: project.ID, CallerID: memberID, Update: appmodel.ProjectUpdate{Name: "Changed", Color: "", Archived: nil, EstimateMinutes: nil}}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("UpdateProject error = %v, want forbidden", err)
	}
	if err := d.DeleteProject(ctx, appmodel.ProjectMutationRequest{TeamID: teamID, ProjectID: project.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("DeleteProject error = %v, want forbidden", err)
	}

	got, err := d.GetProjectByID(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "P" {
		t.Errorf("unauthorized writes changed project name to %q", got.Name)
	}
	if _, err := d.GetProjectBySlug(ctx, appmodel.ProjectSlugQuery{TeamID: teamID, Slug: "unauthorized"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unauthorized create left a project behind: %v", err)
	}
}

func TestTeamSettingsWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	denied := []struct {
		name string
		call func() error
	}{
		{"rename", func() error {
			return d.RenameTeam(ctx, appmodel.TeamRenameRequest{TeamID: teamID, CallerID: memberID, Name: "Changed"})
		}},
		{"billing", func() error {
			return d.SetTeamBilling(ctx, appmodel.TeamBillingRequest{TeamID: teamID, CallerID: memberID, Rules: model.BillingRules{RoundMinutes: 15, RoundMode: "up", InvoicePrefix: "INV"}})
		}},
		{"currency", func() error {
			return d.SetTeamCurrency(ctx, appmodel.TeamCurrencyRequest{TeamID: teamID, CallerID: memberID, Currency: "EUR"})
		}},
		{"requisites", func() error {
			return d.SetTeamRequisites(ctx, appmodel.TeamRequisitesRequest{TeamID: teamID, CallerID: memberID, Requisites: "changed", VATNote: "changed"})
		}},
		{"logo", func() error {
			return d.SetTeamLogo(ctx, appmodel.TeamLogoRequest{TeamID: teamID, CallerID: memberID, DataURL: "data:image/png;base64,AA=="})
		}},
		{"modules", func() error {
			return d.SetTeamModules(ctx, appmodel.TeamModulesRequest{TeamID: teamID, CallerID: memberID, Selected: map[string]bool{"reports": true}})
		}},
	}
	for _, test := range denied {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, model.ErrForbidden) {
				t.Fatalf("write error = %v, want forbidden", err)
			}
		})
	}

	team, err := d.FindTeam(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if team.Name != "T" {
		t.Errorf("unauthorized rename changed team name to %q", team.Name)
	}
	currency, err := d.TeamCurrency(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if currency != "RUB" {
		t.Errorf("unauthorized write changed team currency to %q", currency)
	}
}

func TestTeamCurrency_PropagatesDatabaseErrors(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Currency read error", "currency-read-error")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if currency, err := d.TeamCurrency(ctx, teamID); !errors.Is(err, context.Canceled) {
		t.Fatalf("TeamCurrency(%d) = %q, %v; want database cancellation error", teamID, currency, err)
	}
}

// seedTeam inserts a user + team row and returns the team id. The
// teams table has FK owner_id → users(id), so a user row is required
// before any project in the test DB can exist.
func seedTeam(t *testing.T, d *DB, name, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	var uid int64
	err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?) RETURNING id`,
		"owner-"+slug+"@test.local", "x", "Owner").Scan(&uid)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	var id int64
	err = d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO teams (slug, name, owner_id) VALUES (?, ?, ?) RETURNING id`, slug, name, uid).Scan(&id)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)`, id, uid, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("seed team membership: %v", err)
	}
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

func teamOwner(t *testing.T, d *DB, teamID int64) int64 {
	t.Helper()
	team, err := d.FindTeam(t.Context(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	return team.OwnerID
}

func seedProjectTestUser(t *testing.T, d *DB, name string) int64 {
	t.Helper()
	var userID int64
	err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?) RETURNING id`,
		name+"@test.local", "x", name).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	return userID
}

func boolPtr(b bool) *bool { return &b }
