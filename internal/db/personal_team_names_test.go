package db

import "testing"

// Registration named a personal workspace with the localized prefix. The
// backfill drops the prefix, and leaves alone a workspace its owner renamed.
func TestPersonalTeamNamesDropTheRegistrationPrefix(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	seed := func(email, owner, team, slug string) {
		t.Helper()
		var uid int64
		if err := d.TestSQL().QueryRowContext(ctx,
			`INSERT INTO users (email, password_hash, name) VALUES (?, 'x', ?) RETURNING id`,
			email, owner).Scan(&uid); err != nil {
			t.Fatalf("seed user %q: %v", owner, err)
		}
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO teams (slug, name, owner_id) VALUES (?, ?, ?)`, slug, team, uid); err != nil {
			t.Fatalf("seed team %q: %v", team, err)
		}
	}
	seed("ru-owner@test.local", "Иван Петров", "Пространство: Иван Петров", "ru-owner")
	seed("en-owner@test.local", "Ann Lee", "Ann Lee's workspace", "en-owner")
	seed("renamed@test.local", "Мария", "Студия Мария", "renamed-owner")

	// openTestDB runs the startup sequence, which has already recorded this
	// backfill. Clearing the marker lets the test run it on its own rows.
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = $1`, "20261016_personal_team_names"); err != nil {
		t.Fatal(err)
	}
	if err := d.migratePersonalTeamNames(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	want := map[string]string{
		"ru-owner":      "Иван Петров",
		"en-owner":      "Ann Lee",
		"renamed-owner": "Студия Мария",
	}
	for slug, name := range want {
		var got string
		if err := d.TestSQL().QueryRowContext(ctx, `SELECT name FROM teams WHERE slug = ?`, slug).Scan(&got); err != nil {
			t.Fatalf("read team %s: %v", slug, err)
		}
		if got != name {
			t.Errorf("team %s = %q, want %q", slug, got, name)
		}
	}
}
