package db

import (
	"context"
	"testing"
)

// The night gate (UX-33) reads each member's hand-set zone from the "tz" key
// of their preferences row: present for someone who chose, empty for those who
// never did, empty instead of broken for a zone the tz database stopped
// knowing. A row without preferences at all reads the same.
func TestUserTimeZonesReadTheHandSetZone(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	insertUser := func(t *testing.T, email string, prefs string) int64 {
		t.Helper()
		var id int64
		if err := d.TestSQL().QueryRowContext(ctx,
			`INSERT INTO users (email, password_hash, name, prefs) VALUES (?, 'x', 'M', ?) RETURNING id`,
			email, prefs).Scan(&id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		return id
	}
	chose := insertUser(t, "tz-chose@test.local", `{"tz":"Asia/Tokyo","lang":"ru"}`)
	blank := insertUser(t, "tz-blank@test.local", `{"lang":"en"}`)
	none := insertUser(t, "tz-none@test.local", "")

	zones, err := d.UserTimeZones(ctx, []int64{chose, blank, none})
	if err != nil {
		t.Fatalf("UserTimeZones: %v", err)
	}
	if zones[chose] != "Asia/Tokyo" {
		t.Fatalf("zone = %q, want the hand-set Tokyo", zones[chose])
	}
	if zones[blank] != "" || zones[none] != "" {
		t.Fatalf("zones = %q/%q, want empty for members who never chose", zones[blank], zones[none])
	}

	// A zone the tz database stopped knowing reads like an absent one: the
	// member falls back to the workspace's answer instead of breaking a pass.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE users SET prefs = '{"tz":"Mars/Olympus_Mons"}' WHERE id = ?`, chose); err != nil {
		t.Fatalf("seed bad zone: %v", err)
	}
	zones, err = d.UserTimeZones(ctx, []int64{chose})
	if err != nil {
		t.Fatalf("UserTimeZones with a zone the tz database stopped knowing: %v", err)
	}
	if zones[chose] != "Mars/Olympus_Mons" {
		t.Fatalf("zone = %q, want the raw value — deciding on it is the reminders' job", zones[chose])
	}
}
