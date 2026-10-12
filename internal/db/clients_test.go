package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestCreateClient_RejectsDuplicateNameInTeam(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	owner := teamOwner(t, d, teamID)

	if _, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: teamID, CallerID: owner, Name: "Ромашка"}); err != nil {
		t.Fatalf("first client: %v", err)
	}
	_, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: teamID, CallerID: owner, Name: "Ромашка"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name in one team should be rejected, got %v", err)
	}
}

// The same customer name in two workspaces is two different customers.
func TestCreateClient_AllowsSameNameAcrossTeams(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	first := seedTeam(t, d, "First", "first")
	second := seedTeam(t, d, "Second", "second")

	if _, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: first, CallerID: teamOwner(t, d, first), Name: "Ромашка"}); err != nil {
		t.Fatalf("first workspace: %v", err)
	}
	if _, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: second, CallerID: teamOwner(t, d, second), Name: "Ромашка"}); err != nil {
		t.Fatalf("same name in another workspace must be allowed, got %v", err)
	}
}

// clients.name is CITEXT, so a differently-cased duplicate must collide too.
func TestCreateClient_RejectsCaseVariantInTeam(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	owner := teamOwner(t, d, teamID)

	if _, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: teamID, CallerID: owner, Name: "ООО Ромашка"}); err != nil {
		t.Fatalf("first client: %v", err)
	}
	if _, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: teamID, CallerID: owner, Name: "ооо ромашка"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("case variant must collide with the existing card, got %v", err)
	}
}

func TestGetClientInTeam_HidesOtherTeamsClient(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	mine := seedTeam(t, d, "Mine", "mine")
	theirs := seedTeam(t, d, "Theirs", "theirs")

	card, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: theirs, CallerID: teamOwner(t, d, theirs), Name: "Чужая"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetClientInTeam(ctx, appmodel.ClientScopeQuery{TeamID: mine, ClientID: card.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another workspace's client must read as not found, got %v", err)
	}
}

// The client id arrives from a form, so linking someone else's card must fail
// rather than silently attach the project to a stranger's company.
func TestUpdateProject_RejectsClientFromAnotherTeam(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	mine := seedTeam(t, d, "Mine", "mine")
	theirs := seedTeam(t, d, "Theirs", "theirs")

	card, err := d.CreateClient(ctx, appmodel.ClientCreateRequest{TeamID: theirs, CallerID: teamOwner(t, d, theirs), Name: "Чужая"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: mine, CallerID: teamOwner(t, d, mine), Name: "Site"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := card.ID
	_, err = d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{
		TeamID: mine, ProjectID: p.ID, CallerID: teamOwner(t, d, mine),
		Update: appmodel.ProjectUpdate{Name: "Site", Color: p.Color, ClientID: &foreign},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("linking another workspace's client must fail, got %v", err)
	}
}

// Clearing the budget field must remove the ceiling, not set it to zero — a
// zero ceiling would read as "nothing may be billed".
func TestUpdateProject_BudgetZeroClearsCeiling(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	budget := 250_000_00
	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{
		TeamID: teamID, CallerID: teamOwner(t, d, teamID), Name: "Site", BudgetCents: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.BudgetCents == nil || *p.BudgetCents != budget {
		t.Fatalf("expected budget %d on create, got %v", budget, p.BudgetCents)
	}

	zero := 0
	updated, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{
		TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID),
		Update: appmodel.ProjectUpdate{Name: "Site", Color: p.Color, BudgetCents: &zero},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.BudgetCents != nil {
		t.Fatalf("clearing the budget must unset it, got %d", *updated.BudgetCents)
	}

	// And an absent pointer must leave the ceiling untouched.
	kept, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{
		TeamID: teamID, ProjectID: p.ID, CallerID: teamOwner(t, d, teamID),
		Update: appmodel.ProjectUpdate{Name: "Renamed", Color: p.Color},
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept.BudgetCents != nil {
		t.Fatalf("a field the form never sent must not change the budget, got %d", *kept.BudgetCents)
	}
}

// Existing recipients live in free-text project columns; the backfill has to
// turn them into cards without losing the link or the snapshot.
func TestMigrateProjectsToClients_LinksExistingRecipients(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	// Written the way the product used to store a recipient: straight into
	// the columns, since no service writes them any more.
	now := FormatTime(d.currentTime().UTC())
	for _, spec := range []struct{ slug, name, email string }{
		{"site", "Ромашка", "buh@romashka.ru"},
		{"shop", "Ромашка", "buh@romashka.ru"},
		{"other", "Другой", ""},
	} {
		if _, err := d.sql.ExecContext(ctx,
			`INSERT INTO projects (team_id, slug, name, color, client_name, client_email, created_at, updated_at)
			 VALUES (?, ?, ?, '#7c8499', ?, ?, ?, ?)`,
			teamID, spec.slug, spec.slug, spec.name, spec.email, now, now); err != nil {
			t.Fatalf("seed project %s: %v", spec.slug, err)
		}
	}
	// openTestDB runs the whole startup sequence, which has already recorded
	// this backfill. Clearing the marker puts the database back into the
	// state it was in before the backfill existed.
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = $1`, "20261012_projects_to_clients"); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateProjectsToClients(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	cards, err := d.ListClients(ctx, appmodel.ClientCatalogQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("two distinct recipients should become two cards, got %d: %+v", len(cards), cards)
	}
	byName := map[string]model.Client{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	romashka, ok := byName["Ромашка"]
	if !ok {
		t.Fatalf("card for Ромашка missing: %+v", cards)
	}
	if romashka.Email != "buh@romashka.ru" {
		t.Errorf("card email = %q, want the project's address", romashka.Email)
	}

	projects, err := d.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	// Each project must land on the card for the recipient it actually names.
	wantClientFor := map[string]string{"site": "Ромашка", "shop": "Ромашка", "other": "Другой"}
	if len(projects) != len(wantClientFor) {
		t.Fatalf("expected %d projects, got %d", len(wantClientFor), len(projects))
	}
	for _, p := range projects {
		want, known := wantClientFor[p.Slug]
		if !known {
			t.Fatalf("unexpected project %q", p.Slug)
		}
		card, known := byName[want]
		if !known {
			t.Fatalf("no card for %q", want)
		}
		if p.ClientID != card.ID {
			t.Errorf("project %q linked to client %d, want %d (%s)", p.Slug, p.ClientID, card.ID, want)
		}
		// The snapshot columns are what an already-sent invoice shows, so the
		// backfill must not blank them out.
		if p.Name == "" {
			t.Errorf("project %q lost its recipient snapshot", p.Slug)
		}
	}
}

// Re-running the backfill must not fork a customer into two cards.
func TestMigrateProjectsToClients_IsRepeatable(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")

	now := FormatTime(d.currentTime().UTC())
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, client_name, created_at, updated_at)
		 VALUES (?, 'site', 'site', '#7c8499', 'Ромашка', ?, ?)`, teamID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = $1`, "20261012_projects_to_clients"); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateProjectsToClients(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := d.ListClients(ctx, appmodel.ClientCatalogQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.migrateProjectsToClients(ctx); err != nil {
		t.Fatalf("second run must be a no-op, got %v", err)
	}
	second, err := d.ListClients(ctx, appmodel.ClientCatalogQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) || len(second) != 1 {
		t.Fatalf("repeat run changed the cards: %d -> %d", len(first), len(second))
	}
}
