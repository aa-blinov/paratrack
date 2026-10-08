package db

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestLegacyActivityWritesRejectWorkspaceScope(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Activity actor", "activity-actor")
	ownerID := teamOwner(t, d, teamID)
	if _, err := d.createActivity(ctx, legacyActivityRequest{TeamID: teamID, Name: "blocked"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("activity create without actor = %v, want forbidden", err)
	}
	if _, err := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: teamID, Name: "blocked"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("activity resolution without actor = %v, want forbidden", err)
	}
	if _, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "allowed"}); err != nil {
		t.Fatalf("activity create with current owner = %v", err)
	}
}

func TestActivityKeyBackfillProcessesMultipleBatches(t *testing.T) {
	d := openTestDB(t)
	const count = migrationBatchSize*2 + 1
	if _, err := d.TestSQL().ExecContext(t.Context(), `INSERT INTO activities (name, team_id, name_key)
		SELECT 'legacy-' || n, NULL, NULL FROM generate_series(1, ?) AS series(n)`, count); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateActivityKeys(t.Context()); err != nil {
		t.Fatal(err)
	}
	var missing int
	if err := d.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM activities WHERE name LIKE 'legacy-%' AND name_key IS NULL`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("activities without a backfilled key = %d, want 0", missing)
	}
}

func TestGetOrCreateActivity_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()

	a, err := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "work" {
		t.Errorf("expected normalised name 'work', got %q", a.Name)
	}

	// Different case → same id, canonical lowercase name.
	for _, variant := range []string{"WORK", "Work", "wOrK"} {
		got, err := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: variant})
		if err != nil {
			t.Errorf("getOrCreateActivity(%q): %v", variant, err)
			continue
		}
		if got.ID != a.ID || got.Name != "work" {
			t.Errorf("variant %q: got id=%d name=%q, want id=%d name='work'",
				variant, got.ID, got.Name, a.ID)
		}
	}
}

func TestGetOrCreateActivityForMember_ReusesActivityMissingNameKey(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Activity backfill", "activity-name-key-backfill")
	ownerID := teamOwner(t, d, teamID)
	var legacyID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id, created_at, updated_at) VALUES (?, ?, ?, ?) RETURNING id`,
		"Legacy Work", teamID, FormatTime(time.Now().UTC()), FormatTime(time.Now().UTC())).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}

	found, err := d.getActivityByName(ctx, teamID, "LEGACY WORK")
	if err != nil {
		t.Fatalf("lookup legacy activity without key: %v", err)
	}
	if found.ID != legacyID {
		t.Fatalf("legacy activity lookup id = %d, want %d", found.ID, legacyID)
	}
	resolved, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "legacy work"})
	if err != nil {
		t.Fatalf("resolve legacy activity: %v", err)
	}
	if resolved.ID != legacyID {
		t.Fatalf("resolved activity id = %d, want existing id %d", resolved.ID, legacyID)
	}
	var nameKey string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT name_key FROM activities WHERE id = ?`, legacyID).Scan(&nameKey); err != nil {
		t.Fatal(err)
	}
	if nameKey != "legacy work" {
		t.Fatalf("backfilled name_key = %q, want %q", nameKey, "legacy work")
	}
}

func TestGetActivityByName_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "Reading"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Reading" {
		t.Errorf("the name keeps the case it was typed in, got %q", a.Name)
	}
	b, err := d.getActivityByName(ctx, 0, "READING")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("case-insensitive lookup should find same id, got %d vs %d", a.ID, b.ID)
	}
}

func TestCreateActivity_TrimsAndKeepsCase(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.createActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "  Writing  "})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Writing" {
		t.Errorf("expected trimmed 'Writing', got %q", a.Name)
	}
	// Cyrillic folds too: "Вёрстка" and "вёрстка" are one activity.
	x, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "Вёрстка"})
	y, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "вёрстка"})
	if x.ID != y.ID || y.Name != "Вёрстка" {
		t.Errorf("Cyrillic case: %d/%q vs %d/%q", x.ID, x.Name, y.ID, y.Name)
	}
}

// An activity's mark used to be hashed from its name into ten slots, so a
// workspace with more than ten activities gave two of them the same colour
// and the dot stopped meaning anything. Colours are stored now, and a new
// activity must not repeat one the workspace already wears.
func TestActivityColorsDoNotRepeatWithinWorkspace(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	team := seedTeam(t, d, "Colour owner", "colour-owner")
	owner := teamOwner(t, d, team)
	names := []string{"reading", "writing", "coding", "review", "planning",
		"standup", "retrospective", "design", "testing", "release"}
	var ids []int64
	for _, name := range names {
		activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{
			TeamID: team, CallerID: owner, Name: name})
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if strings.TrimSpace(activity.Color) == "" {
			t.Fatalf("%q was created without a colour", name)
		}
		ids = append(ids, activity.ID)
	}
	seen := map[string]string{}
	for _, id := range ids {
		var color string
		if err := d.TestSQL().QueryRowContext(ctx,
			`SELECT color FROM activities WHERE id = ?`, id).Scan(&color); err != nil {
			t.Fatalf("read colour: %v", err)
		}
		if other, clash := seen[color]; clash {
			t.Errorf("activities %q and id %d share %s — the mark identifies nothing", other, id, color)
		}
		seen[color] = fmt.Sprint(id)
	}
	if len(seen) != len(names) {
		t.Errorf("expected %d distinct colours, got %d", len(names), len(seen))
	}
}

// Before the migration every row has no colour, and the hash gives several of
// them the same one. The backfill has to leave the workspace with as many
// distinct marks as the palette allows — and it has to do it without moving
// the activities whose mark was already unambiguous.
func TestActivityColorBackfillSeparatesCollidingRows(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	team := seedTeam(t, d, "Backfill owner", "backfill-owner")
	names := []string{"reading", "writing", "coding", "review", "planning",
		"standup", "retrospective", "design", "testing", "release", "billing", "onboarding"}
	for _, name := range names {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO activities (name, name_key, team_id, created_at, updated_at)
			 VALUES (?, ?, ?, '2026-01-01T00:00:00.000', '2026-01-01T00:00:00.000')`,
			name, strings.ToLower(name), team); err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
	}
	// How many collided before the backfill ran.
	before := map[string]int{}
	for _, name := range names {
		before[model.ColorForName(name)]++
	}
	shared := 0
	for _, n := range before {
		if n > 1 {
			shared++
		}
	}
	if shared == 0 {
		t.Skip("this sample happens to hash to distinct slots; nothing to separate")
	}
	if err := d.migrateActivityColors(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	after := map[string]int{}
	rows, err := d.TestSQL().QueryContext(ctx, `SELECT color FROM activities WHERE team_id = ?`, team)
	if err != nil {
		t.Fatalf("read colours: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var color string
		if err := rows.Scan(&color); err != nil {
			t.Fatalf("scan colour: %v", err)
		}
		after[color]++
	}
	// The palette is the ceiling, and it is the honest one: measured, more
	// colours stop being tellable apart long before they stop repeating. So
	// the backfill's promise is that every slot is used, not that there are
	// more marks than the palette has.
	want := min(len(names), len(model.Palette))
	if len(after) != want {
		t.Errorf("expected %d distinct marks for %d activities over a %d-colour palette, got %d",
			want, len(names), len(model.Palette), len(after))
	}
	if len(after) <= shared {
		t.Errorf("the backfill separated nothing: %d distinct slots before, %d after", shared+1, len(after))
	}
}
