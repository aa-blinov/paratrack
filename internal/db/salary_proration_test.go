package db

import (
	"strings"
	"testing"
	"time"
)

// A month a salaried member was away for part of pays the share of its
// working days they worked: June 2026 has 22 weekdays, 12 June is a
// holiday, so 21 are to be worked, and five vacation days leave 16.
func TestSalaryFollowsTheMonthsWorkingDays(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Оклад", "salary-days")
	ownerID := teamOwner(t, d, teamID)
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET pay_kind = 'monthly', monthly_pay_cents = 2100000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO team_holidays (team_id, day, name, kind) VALUES (?, '2026-06-12', 'День России', 'off')`, teamID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	full, err := buildPayrollLines(ctx, d.TestSQL(), teamID, start, end, false, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || full[0].AmountCents != 2100000 || strings.Contains(full[0].Label, "дн.") {
		t.Fatalf("a month without absences: %+v", full)
	}
	for _, day := range []string{"2026-06-15", "2026-06-16", "2026-06-17", "2026-06-18", "2026-06-19"} {
		if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO member_absences (team_id, user_id, day, kind, note, created_at) VALUES (?, ?, ?, 'vacation', '', ?)`, teamID, ownerID, day, FormatTime(time.Now().UTC())); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := buildPayrollLines(ctx, d.TestSQL(), teamID, start, end, false, end)
	if err != nil {
		t.Fatal(err)
	}
	// 22 weekdays in June 2026, less 12 June: 21 to be worked, 16 worked.
	want := (2100000*16 + 21/2) / 21
	if len(lines) != 1 || lines[0].AmountCents != want || !strings.Contains(lines[0].Label, "16 из 21 дн.") {
		t.Fatalf("a month with five vacation days: %+v, want %d", lines, want)
	}
}
