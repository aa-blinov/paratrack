package db

import (
	"fmt"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// An existing workspace that chose its sections gains the tasks section once.
// A workspace that never chose (empty) or chose nothing ("none") is left alone,
// and a second start of the schema changes nothing.
func TestTasksSectionIsAddedToChosenSetsOnly(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "tasks-migrate@test.local")
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

	for run := 1; run <= 2; run++ {
		if err := d.applySchemaContext(ctx); err != nil {
			t.Fatalf("schema start %d: %v", run, err)
		}
		for i, c := range cases {
			var got string
			if err := d.TestSQL().QueryRowContext(ctx, `SELECT modules FROM teams WHERE id = ?`, ids[i]).Scan(&got); err != nil {
				t.Fatalf("read modules for %q: %v", c.name, err)
			}
			if got != c.want {
				t.Fatalf("after schema start %d, %s has modules %q, want %q", run, c.name, got, c.want)
			}
		}
	}
}
