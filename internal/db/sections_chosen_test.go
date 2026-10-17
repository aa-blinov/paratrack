package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A workspace made now waits for its sections choice; a workspace that existed
// before the column keeps the menu it had; saving a choice marks it chosen.
func TestSectionsChoiceStartsOpenForNewWorkspacesOnly(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	var owner int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ('chosen-owner@test.local', 'x', 'Владелец') RETURNING id`).Scan(&owner); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var legacy int64
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO teams (slug, name, owner_id) VALUES ('legacy-team', 'Старое пространство', ?)`, owner); err != nil {
		t.Fatalf("seed legacy team: %v", err)
	}
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT id FROM teams WHERE slug = 'legacy-team'`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}

	fresh, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "fresh-team", Name: "Новое пространство", OwnerID: owner})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if chosen, err := d.TeamSectionsChosen(ctx, fresh); err != nil || chosen {
		t.Fatalf("new workspace chosen = %v, %v; want false", chosen, err)
	}
	if chosen, err := d.TeamSectionsChosen(ctx, legacy); err != nil || !chosen {
		t.Fatalf("existing workspace chosen = %v, %v; want true", chosen, err)
	}

	if err := d.SetTeamModules(ctx, appmodel.TeamModulesRequest{TeamID: fresh, CallerID: owner, Selected: map[string]bool{"graph": true}}); err != nil {
		t.Fatalf("save sections: %v", err)
	}
	if chosen, err := d.TeamSectionsChosen(ctx, fresh); err != nil || !chosen {
		t.Fatalf("after save chosen = %v, %v; want true", chosen, err)
	}
}
