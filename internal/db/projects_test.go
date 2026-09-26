package db

import (
	"context"
	"strings"
	"testing"
)

func TestCreateProject_Defaults(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	// Existing team is required by the FK. Use team 1 (seeded by Open()
	// would be wrong here — openTestDB uses :memory:, no seed). Insert one.
	teamID := seedTeam(t, d, "Test Team", "tt")

	p, err := d.CreateProject(ctx, teamID, "EORA RAG", "", "")
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

	p, err := d.CreateProject(ctx, teamID, "Side Project", "side", "#7c3aed")
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

	if _, err := d.CreateProject(ctx, teamID, "First", "shared", ""); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreateProject(ctx, teamID, "Second", "shared", "")
	if err != ErrDuplicate {
		t.Errorf("expected ErrDuplicate on slug collision, got %v", err)
	}
}

func TestCreateProject_RejectsBadColor(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	for _, bad := range []string{"red", "#abc", "#abcd", "7c3aed"} {
		if _, err := d.CreateProject(ctx, teamID, "x", "y", bad); err == nil {
			t.Errorf("expected error for bad color %q", bad)
		}
	}
}

func TestGetProjectBySlug(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	created, err := d.CreateProject(ctx, teamID, "EORA RAG", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.GetProjectBySlug(ctx, teamID, "EORA-RAG") // case-insensitive
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

	a, _ := d.CreateProject(ctx, teamID, "Alpha", "", "")
	b, _ := d.CreateProject(ctx, teamID, "Bravo", "", "")
	if _, err := d.UpdateProject(ctx, teamID, a.ID, "", "", boolPtr(true), nil); err != nil {
		t.Fatal(err)
	}

	// Default: archived hidden.
	list, err := d.ListProjects(ctx, teamID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("expected only Bravo, got %d entries", len(list))
	}

	// includeArchived: both visible, archived last.
	all, err := d.ListProjects(ctx, teamID, true)
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
	p, _ := d.CreateProject(ctx, teamID, "EORA RAG", "eora", "#aaaaaa")

	// Only color.
	upd, err := d.UpdateProject(ctx, teamID, p.ID, "", "#bbbbbb", nil, nil)
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
	upd, err = d.UpdateProject(ctx, teamID, p.ID, "EORA v2", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "EORA v2" || upd.Color != "#bbbbbb" {
		t.Errorf("partial update wrong: %+v", upd)
	}

	// Only archived.
	upd, err = d.UpdateProject(ctx, teamID, p.ID, "", "", boolPtr(true), nil)
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
	p, _ := d.CreateProject(ctx, teamA, "Owned by A", "owned-a", "")

	// Team B trying to rename should fail (not see it as theirs).
	_, err := d.UpdateProject(ctx, teamB, p.ID, "Hacked", "", nil, nil)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound on cross-team update, got %v", err)
	}
}

func TestDeleteProject_PreservesActivities(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	p, _ := d.CreateProject(ctx, teamID, "X", "x", "")
	a, err := d.CreateActivity(ctx, teamID, "writing")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, teamID, a.ID, p.ID); err != nil {
		t.Fatal(err)
	}

	// Delete the project; the activity should survive with project_id=NULL.
	if err := d.DeleteProject(ctx, teamID, p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetActivity(ctx, a.ID)
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
	p, _ := d.CreateProject(ctx, teamA, "x", "x", "")

	if err := d.DeleteProject(ctx, teamB, p.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAssignActivityProject_CrossTeamForbidden(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "A", "a")
	teamB := seedTeam(t, d, "B", "b")
	// Project lives in A.
	p, _ := d.CreateProject(ctx, teamA, "x", "x", "")
	// Activity lives in B.
	_, err := d.CreateActivity(ctx, teamB, "writing")
	if err != nil {
		t.Fatal(err)
	}
	aInB, err := d.GetActivityByName(ctx, teamB, "writing")
	if err != nil {
		t.Fatal(err)
	}

	// Team B tries to attach A's project to its activity — must fail.
	if err := d.AssignActivityProject(ctx, teamB, aInB.ID, p.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound on cross-team assign, got %v", err)
	}

	// Same team, valid assign.
	if err := d.AssignActivityProject(ctx, teamB, aInB.ID, 0); err != nil {
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
	p, _ := d.CreateProject(ctx, teamID, "P", "p", "")

	a1, _ := d.CreateActivity(ctx, teamID, "writing")
	a2, _ := d.CreateActivity(ctx, teamID, "reading")
	// Activity without project.
	_, _ = d.CreateActivity(ctx, teamID, "unrelated")
	_ = d.AssignActivityProject(ctx, teamID, a1.ID, p.ID)
	_ = d.AssignActivityProject(ctx, teamID, a2.ID, p.ID)

	list, err := d.ListActivitiesForProject(ctx, p.ID, false)
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

// seedTeam inserts a user + team row and returns the team id. The
// teams table has FK owner_id → users(id), so a user row is required
// before any project in the test DB can exist.
func seedTeam(t *testing.T, d *DB, name, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	ures, err := d.sql.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?)`,
		"owner-"+slug+"@test.local", "x", "Owner")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	uid, _ := ures.LastInsertId()

	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO teams (slug, name, owner_id) VALUES (?, ?, ?)`, slug, name, uid)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

func boolPtr(b bool) *bool { return &b }
