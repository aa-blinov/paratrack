package db

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

// The journal names an account by its display name, or by its address when the name
// is empty, and an id with no account gets no name at all.
func TestUserLabelsNamesAccountsAndSkipsUnknownIDs(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	named := seedProjectTestUser(t, d, "Мария")
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE users SET name = '' WHERE id = ?`, named); err != nil {
		t.Fatal(err)
	}
	labelled := seedProjectTestUser(t, d, "Иван")

	labels, err := d.UserLabels(ctx, []int64{named, labelled, 999999})
	if err != nil {
		t.Fatalf("user labels: %v", err)
	}
	if labels[named] != "Мария@test.local" {
		t.Fatalf("a user with no name is labelled %q, want their address", labels[named])
	}
	if labels[labelled] != "Иван" {
		t.Fatalf("a named user is labelled %q, want the name", labels[labelled])
	}
	if _, ok := labels[999999]; ok {
		t.Fatal("an id with no account got a label")
	}
	if empty, err := d.UserLabels(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids = %v, %v; want an empty result", empty, err)
	}
}

// The filter's people are everyone who left a row in this workspace, and a member who
// has since left still has their rows. Another workspace's rows do not count.
func TestAuditActorsIncludeFormerMembersOfThisWorkspaceOnly(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Журнал", "actors-trail")
	ownerID := teamOwner(t, d, teamID)
	former := seedProjectTestUser(t, d, "former-actor")
	stranger := seedProjectTestUser(t, d, "other-workspace-actor")
	otherTeam := seedTeam(t, d, "Чужой", "actors-other")
	if err := d.Audit(ctx, model.AuditRecord{TeamID: teamID, UserID: ownerID, Action: "team.modules", Target: "tasks", Meta: "", IP: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Audit(ctx, model.AuditRecord{TeamID: teamID, UserID: former, Action: "invoice.create", Target: "INV-2026-500", IP: "10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Audit(ctx, model.AuditRecord{TeamID: otherTeam, UserID: stranger, Action: "invoice.create", Target: "INV-OTHER", IP: "10.0.0.3"}); err != nil {
		t.Fatal(err)
	}

	actors, err := d.ListAuditActors(ctx, teamID)
	if err != nil {
		t.Fatalf("audit actors: %v", err)
	}
	want := map[int64]bool{ownerID: true, former: true}
	if len(actors) != len(want) {
		t.Fatalf("actors = %v, want the owner and the former member", actors)
	}
	for _, id := range actors {
		if !want[id] {
			t.Fatalf("actors = %v includes %d, who left no row here", actors, id)
		}
	}
	if none, err := d.ListAuditActors(ctx, seedTeam(t, d, "Пустой", "actors-empty")); err != nil || len(none) != 0 {
		t.Fatalf("an empty journal has actors %v, %v", none, err)
	}
}
