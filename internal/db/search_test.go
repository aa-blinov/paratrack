package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The global search finds a workspace's projects, clients, tasks, tags and the viewer's
// own sessions. Members do not see client emails or other people; managers see both.
// Archived projects and clients stay out, and % in the text matches itself (UI-01).
func TestGlobalSearchRespectsWhatTheViewerMaySee(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Поиск", "global-search")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "search-member")
	addMember(t, d, teamID, memberID, "member")
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Лендинг Ромашка", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO clients (team_id, name, email) VALUES (?, 'Ромашка', 'buh@romashka.test')`, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO clients (team_id, name, email, archived) VALUES (?, 'Ромашка архив', '', 1)`, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Вёрстка ромашки", Status: "todo"}); err != nil {
		t.Fatal(err)
	}
	activity := newTestActivity(t, d, ctx, teamID, memberID, "Ромашка звонок")
	seedClosedSession(t, d, teamID, memberID, activity, time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC))

	member, err := d.SearchWorkspace(ctx, appmodel.SearchQuery{TeamID: teamID, UserID: memberID, Text: "ромашк", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(member.Projects) != 1 || len(member.Clients) != 1 || len(member.Tasks) != 1 || len(member.Sessions) != 1 {
		t.Fatalf("the member's search holds %+v, want one each of project, client, task and session", member)
	}
	if member.Clients[0].Detail != "" {
		t.Fatalf("a member saw a client's email in search: %q", member.Clients[0].Detail)
	}
	if len(member.People) != 0 {
		t.Fatal("a member found people in search")
	}

	owner, err := d.SearchWorkspace(ctx, appmodel.SearchQuery{TeamID: teamID, UserID: ownerID, Manager: true, Text: "romashka", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.Clients) != 1 || owner.Clients[0].Detail != "buh@romashka.test" {
		t.Fatalf("the manager's client search holds %+v, want the client with its email", owner.Clients)
	}
	if len(owner.Sessions) != 0 {
		t.Fatal("the owner found the member's session: sessions are only the viewer's own")
	}

	literal, err := d.SearchWorkspace(ctx, appmodel.SearchQuery{TeamID: teamID, UserID: memberID, Text: "%", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(literal.Projects)+len(literal.Clients)+len(literal.Tasks) != 0 {
		t.Fatal("a percent sign matched every name instead of itself")
	}
}
