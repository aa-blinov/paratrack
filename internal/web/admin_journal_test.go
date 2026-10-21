package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// The owner changes a member's role and then removes that member. The journal names
// the member on both rows and in the filter, and the filter narrows to what the
// member left behind, even though the member is no longer in the workspace.
func TestJournalNamesWhoChangedAndKeepsFormerMembersFilterable(t *testing.T) {
	e := newAPIEnv(t)
	e.register("journal-owner@x.test")
	ctx := context.Background()
	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT team_id, user_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatalf("find the owner: %v", err)
	}
	var memberID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ('journal-former@x.test', 'x', 'Бывший Участник') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("seed the member: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("join the member: %v", err)
	}
	if err := e.db.Audit(ctx, model.AuditRecord{TeamID: teamID, UserID: memberID, Action: "invoice.create", Target: "INV-2026-777", IP: "10.0.0.7"}); err != nil {
		t.Fatalf("seed the member's row: %v", err)
	}

	roleResp := e.do("POST", fmt.Sprintf("/api/team/members/%d/role", memberID), url.Values{"role": {"admin"}}, nil)
	roleResp.Body.Close()
	if roleResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("change role: status %d, want a redirect", roleResp.StatusCode)
	}
	removeResp := e.do("POST", fmt.Sprintf("/api/team/members/%d/remove", memberID), url.Values{}, nil)
	removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove member: status %d, want a redirect", removeResp.StatusCode)
	}

	page := reactData[auditPage](t, readBody(t, e.do("GET", "/settings/audit", nil, nil)))
	var roleRow, removeRow *auditRow
	for i := range page.Items {
		switch page.Items[i].Action {
		case "member.role":
			roleRow = &page.Items[i]
		case "member.remove":
			removeRow = &page.Items[i]
		}
	}
	if roleRow == nil || roleRow.Target != "Бывший Участник" || roleRow.Detail != "администратор" || roleRow.Actor == "" {
		t.Fatalf("role change row = %+v, want the member's name and the role", roleRow)
	}
	if removeRow == nil || removeRow.Target != "Бывший Участник" || removeRow.Detail != "" {
		t.Fatalf("removal row = %+v, want the member's name and no detail", removeRow)
	}
	named := false
	for _, person := range page.People {
		if person.ID == memberID && person.Name == "Бывший Участник" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the filter's people %+v do not name the former member", page.People)
	}

	filtered := reactData[auditPage](t, readBody(t, e.do("GET", fmt.Sprintf("/settings/audit?user=%d", memberID), nil, nil)))
	if len(filtered.Items) != 1 || filtered.Items[0].Action != "invoice.create" || filtered.Items[0].Actor != "Бывший Участник" {
		t.Fatalf("filtering by the former member shows %+v, want only their own row", filtered.Items)
	}
}
