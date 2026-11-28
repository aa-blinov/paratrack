package db

import (
	"fmt"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// An existing workspace that chose its sections gains the tasks section once.
// A workspace that never chose (empty) or chose nothing ("none") is left alone.
// A set chosen after that, like the "Studio: start" preset without tasks,
// keeps its shape across restarts: it used to gain ",tasks" on every start.
func TestTasksSectionIsAddedToChosenSetsOnly(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "tasks-migrate@test.local")
	// The template database already ran the migration; forget it so this
	// database plays one that predates the tasks section.
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = 'tasks section for chosen sets'`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, stored, want string
	}{
		{"legacy empty", "", ""},
		{"explicit none", "none", "none"},
		{"chosen set", "graph,goals", "graph,goals,tasks"},
	}
	ids := make([]int64, len(cases))
	for i, c := range cases {
		teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: fmt.Sprintf("tasks-mig-%d", i), Name: c.name, OwnerID: ownerID})
		if err != nil {
			t.Fatalf("create team %q: %v", c.name, err)
		}
		if _, err := d.TestSQL().ExecContext(ctx, `UPDATE teams SET modules = ? WHERE id = ?`, c.stored, teamID); err != nil {
			t.Fatalf("set modules for %q: %v", c.name, err)
		}
		ids[i] = teamID
	}
	if err := d.migrateTasksSection(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(id int64) string {
		var got string
		if err := d.TestSQL().QueryRowContext(ctx, `SELECT modules FROM teams WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	for i, c := range cases {
		if got := read(ids[i]); got != c.want {
			t.Fatalf("%s has modules %q, want %q", c.name, got, c.want)
		}
	}

	// A set chosen later, then a restart: schema pass and data migrations again.
	const studioCore = "graph,tags,invoices,reports,payroll,schedule"
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE teams SET modules = ? WHERE id = ?`, studioCore, ids[2]); err != nil {
		t.Fatal(err)
	}
	if err := d.applySchemaContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateTasksSection(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(ids[2]); got != studioCore {
		t.Fatalf("after a restart the chosen set is %q, want %q", got, studioCore)
	}
}
