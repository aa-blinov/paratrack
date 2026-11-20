package db

import (
	"regexp"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var canonicalTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$`)

func TestFormatTimeKeepsOneWidth(t *testing.T) {
	whole := FormatTime(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	if whole != "2026-11-16T00:00:00.000000000Z" {
		t.Fatalf("whole-second FormatTime = %q", whole)
	}
	fraction := FormatTime(time.Date(2026, 11, 16, 0, 0, 0, 500000000, time.UTC))
	if fraction != "2026-11-16T00:00:00.500000000Z" {
		t.Fatalf("fractional FormatTime = %q", fraction)
	}
	// Same width is the invariant: strings of one width sort by time, strings
	// of mixed widths sort by accident.
	if len(whole) != len(fraction) {
		t.Fatalf("width differs: %q vs %q", whole, fraction)
	}
}

// A week approved to the second must hold a session started half a second
// after its midnight and refuse one started half a second after it ends.
// Before the canonical format the two text shapes sorted '.' before 'Z' and
// both edges leaked.
func TestApprovedWeekBindsFractionalSecondStarts(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "fraction-owner@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "fraction-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	weekStart := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	if err := d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	actor := requestctx.WithTeamID(requestctx.WithActor(ctx, ownerID), teamID)

	inside := weekStart.Add(500 * time.Millisecond)
	if _, err := d.CreateClosedSession(actor, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: inside, End: inside.Add(time.Hour)}); !isApprovalLock(err) {
		t.Fatalf("add half a second into the approved week = %v, want SessionApprovalLockError", err)
	}
	justPast := weekEnd.Add(500 * time.Millisecond)
	if _, err := d.CreateClosedSession(actor, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: justPast, End: justPast.Add(time.Hour)}); err != nil {
		t.Fatalf("add half a second past the approved week = %v, want no refusal", err)
	}
}

func TestMigrateCanonicalTimestampsRewritesLegacyShapes(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "canonical-team", Name: "Команда", OwnerID: seedApprovalUser(t, d, "canonical-owner@test.local")})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	seed := func(number, period string) {
		t.Helper()
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
			 VALUES (?, ?, 'client', ?, ?, 'draft', ?)`,
			teamID, number, period, period, "2026-10-01T00:00:00Z"); err != nil {
			t.Fatalf("seed %s: %v", number, err)
		}
	}
	seed("INV-NOFRACTION", "2026-10-01T00:00:00Z")      // RFC3339Nano with the fraction dropped
	seed("INV-SHORTFRACTION", "2026-10-01T00:00:00.5Z") // a fraction, fewer than nine digits
	seed("INV-DEFAULTSHAPE", "2026-10-01T00:00:00.123") // the schema's old millisecond DEFAULT, no zone
	seed("INV-PYTHONSHAPE", "2026-10-01 00:00:00")      // the Python importer's space-separated form
	seed("INV-GARBAGE", "2026-10-01T99:00:00Z")         // corrupted on purpose; the migration leaves it

	if _, err := d.TestSQL().ExecContext(ctx,
		`DELETE FROM paratrack_migrations WHERE name = '20261120_canonical_timestamps'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateCanonicalTimestamps(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for number, want := range map[string]string{
		"INV-NOFRACTION":    "2026-10-01T00:00:00.000000000Z",
		"INV-SHORTFRACTION": "2026-10-01T00:00:00.500000000Z",
		"INV-DEFAULTSHAPE":  "2026-10-01T00:00:00.123000000Z",
		"INV-PYTHONSHAPE":   "2026-10-01T00:00:00.000000000Z",
		"INV-GARBAGE":       "2026-10-01T99:00:00Z",
	} {
		var period string
		if err := d.TestSQL().QueryRowContext(ctx,
			`SELECT period_start FROM invoices WHERE team_id = ? AND number = ?`, teamID, number).Scan(&period); err != nil {
			t.Fatalf("read %s: %v", number, err)
		}
		if period != want {
			t.Fatalf("%s period = %q, want %q", number, period, want)
		}
	}

	// The DEFAULT the schema once shipped keeps writing rows after the stored
	// ones are rewritten, so it is repointed too and fires canonically.
	var created string
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at) VALUES (?, ?, ?) RETURNING created_at`,
		activityID, teamID, "2026-10-01T00:00:00.000000000Z").Scan(&created); err != nil {
		t.Fatalf("insert with the default created_at: %v", err)
	}
	if !canonicalTimestampPattern.MatchString(created) {
		t.Fatalf("default created_at = %q, want the canonical shape", created)
	}

	// A second run is a no-op: the migration is recorded once.
	if err := d.migrateCanonicalTimestamps(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var applied int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM paratrack_migrations WHERE name = '20261120_canonical_timestamps'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("migration recorded %d times, want once", applied)
	}
}

func TestMigrateCanonicalTimestampsRecordsItselfOnCleanSchema(t *testing.T) {
	// The startup pass on a fresh database is the common case: it records
	// itself and leaves the schema alone.
	ctx := t.Context()
	d := openTestDB(t)
	var applied int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM paratrack_migrations WHERE name = '20261120_canonical_timestamps'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("startup did not record the migration (applied = %d)", applied)
	}
}
