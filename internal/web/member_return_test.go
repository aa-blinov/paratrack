package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// The owner removes a member, finds them under former members on the members page,
// and brings them back as a member. The page then lists them as a member again
// (ADM-04).
func TestRemovedMemberCanBeBroughtBackFromTheMembersPage(t *testing.T) {
	e := newAPIEnv(t)
	e.register("member-return-owner@x.test")
	ctx := context.Background()
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT team_id FROM memberships WHERE role = 'owner' LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("find the workspace: %v", err)
	}
	var memberID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ('member-return@x.test', 'x', 'Вернувшийся') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("seed the member: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', '2026-10-01T09:00:00Z')`,
		teamID, memberID); err != nil {
		t.Fatalf("join the member: %v", err)
	}
	id := strconv.FormatInt(memberID, 10)

	removed := e.do("POST", "/api/team/members/"+id+"/remove", url.Values{}, nil)
	readBody(t, removed)
	if removed.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove the member: status %d", removed.StatusCode)
	}
	page := reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	if len(page.Former) != 1 || page.Former[0].UserID != memberID || page.Former[0].Name != "Вернувшийся" {
		t.Fatalf("the members page lists %+v as former, want the one removed", page.Former)
	}

	returned := e.do("POST", "/api/team/members/"+id+"/return", url.Values{}, nil)
	readBody(t, returned)
	if returned.StatusCode != http.StatusSeeOther || !strings.Contains(returned.Header.Get("Location"), "flash=returned") {
		t.Fatalf("bring the member back: status %d to %q", returned.StatusCode, returned.Header.Get("Location"))
	}
	page = reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	if len(page.Former) != 0 {
		t.Fatalf("the returned member is still listed as former: %+v", page.Former)
	}
	found := false
	for _, member := range page.Members {
		if member.UserID == memberID && member.Role == "member" {
			found = true
		}
	}
	if !found {
		t.Fatal("the returned member is not on the members page as a member")
	}
}
