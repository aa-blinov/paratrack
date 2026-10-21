package web

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Each journal action reads as words: a role change names the member and the role,
// a token names itself, and the codes kept in meta stay off the page.
func TestAuditRowTextsReadEachActionAsWords(t *testing.T) {
	names := map[int64]string{12: "Мария", 7: "Иван Петров"}
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	row := func(action, target, meta string) model.AuditEntry {
		return model.AuditEntry{Action: action, Target: target, Meta: meta, CreatedAt: at}
	}
	cases := []struct {
		name       string
		entry      model.AuditEntry
		teamID     int64
		teamName   string
		wantTarget string
		wantDetail string
	}{
		{"role change names the member and the role", row("member.role", "12", "admin"), 1, "", "Мария", "администратор"},
		{"role change to the owner", row("member.role", "12", "owner"), 1, "", "Мария", "владелец"},
		{"role change to an unknown role keeps the code", row("member.role", "12", "guest"), 1, "", "Мария", "guest"},
		{"role change for a member with no name keeps the id", row("member.role", "99", "member"), 1, "", "99", "участник"},
		{"token creation names the token", row("auth.api_token_create", "1", "sync-laptop"), 1, "", "sync-laptop", ""},
		{"token revocation names the token", row("auth.api_token_delete", "1", "sync-laptop"), 1, "", "sync-laptop", ""},
		{"old revocation without a name keeps the id", row("auth.api_token_delete", "1", ""), 1, "", "1", ""},
		{"member removal keeps the name and hides the code in meta", row("member.remove", "Иван Петров", "user:7"), 1, "", "Иван Петров", ""},
		{"invoice deletion keeps the number and hides the code in meta", row("invoice.delete", "INV-2026-900", "invoice:4"), 1, "", "INV-2026-900", ""},
		{"restore names this workspace", row("workspace.restore", "team:1", "участников: 3, счетов: 5"), 1, "Студия", "Студия", ""},
		{"restore of another workspace names nothing", row("workspace.restore", "team:9", "участников: 3, счетов: 5"), 1, "Студия", "", ""},
		{"other actions keep their target", row("invoice.create", "INV-2026-001", ""), 1, "", "INV-2026-001", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, detail := auditRowTexts(i18n.Ru, tc.entry, names, tc.teamID, tc.teamName)
			if target != tc.wantTarget || detail != tc.wantDetail {
				t.Fatalf("texts = %q / %q, want %q / %q", target, detail, tc.wantTarget, tc.wantDetail)
			}
		})
	}
}

// Only role changes point at a member: the lookup covers their ids and no others.
func TestAuditRoleTargetsAreTheRoleChangesMembers(t *testing.T) {
	list := []model.AuditEntry{
		{Action: "member.role", Target: "12"},
		{Action: "invoice.create", Target: "5"},
		{Action: "member.role", Target: "not-an-id"},
		{Action: "member.role", Target: "30"},
	}
	got := auditRoleTargets(list)
	if len(got) != 2 || got[0] != 12 || got[1] != 30 {
		t.Fatalf("role targets = %v, want [12 30]", got)
	}
}
