package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// joinStudio makes a shared workspace owned by owner and brings each of
// the others (by email) in through an invite link. Returns their user ids.
func joinStudio(t *testing.T, owner *apiEnv, others map[string]*apiEnv, order ...string) []string {
	t.Helper()
	resp := owner.do("POST", "/api/team/create", url.Values{"name": {"Студия"}}, nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		owner.jar[c.Name] = c.Value
	}
	var ids []string
	for _, email := range order {
		o := others[email]
		resp = owner.do("POST", "/api/team/invites", nil, nil)
		loc, _ := url.QueryUnescape(resp.Header.Get("Location"))
		resp.Body.Close()
		token := regexp.MustCompile(`/invites/([A-Za-z0-9_-]+)`).FindStringSubmatch(loc)
		if token == nil {
			t.Fatalf("no invite link in %q", loc)
		}
		resp = o.do("POST", "/api/invites/"+token[1]+"/accept", nil, nil)
		resp.Body.Close()
		for _, c := range resp.Cookies() {
			o.jar[c.Name] = c.Value
		}
		var id string
		o.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = ?`, email).Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func TestSharedThingsAreForManagers(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("own@roles.test")
	adm := newAPIEnvSharedDB(t, owner)
	adm.register("adm@roles.test")
	dev := newAPIEnvSharedDB(t, owner)
	dev.register("dev@roles.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"adm@roles.test": adm, "dev@roles.test": dev}, "adm@roles.test", "dev@roles.test")
	admID, devID := ids[0], ids[1]
	htmx := map[string]string{"HX-Request": "true"}
	resp := owner.do("POST", "/api/team/members/"+admID+"/role", url.Values{"role": {"admin"}}, nil)
	resp.Body.Close()

	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Клиент А"}, "rate": {"3000"}}, nil))
	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Клиент Б"}, "rate": {"9000"}}, nil))
	var pa, pb string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Клиент А'`).Scan(&pa)
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Клиент Б'`).Scan(&pb)

	// A member gives a new activity its first project, but can't move it.
	readBody(t, dev.do("POST", "/api/start", url.Values{"activity": {"вёрстка"}, "project_id": {pa}}, htmx))
	var actID, actProj string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id, project_id FROM activities WHERE name_key = 'вёрстка'`).Scan(&actID, &actProj)
	if actProj != pa {
		t.Fatalf("first project not set: %q", actProj)
	}
	readBody(t, dev.do("POST", "/api/active/stop-all", nil, htmx))
	resp = dev.do("POST", "/api/activities/"+actID+"/project", url.Values{"project_id": {pb}}, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("member moved a shared activity: %d", resp.StatusCode)
	}
	resp = dev.do("POST", "/api/start", url.Values{"activity": {"вёрстка"}, "project_id": {pb}}, htmx)
	resp.Body.Close()
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT project_id FROM activities WHERE id = ?`, actID).Scan(&actProj)
	if actProj != pa {
		t.Errorf("starting a timer moved the activity to another client")
	}
	resp = adm.do("POST", "/api/activities/"+actID+"/project", url.Values{"project_id": {pb}}, nil)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("admin can't move an activity: %d", resp.StatusCode)
	}

	// Tags and goals: members use them, managers remove them.
	readBody(t, owner.do("POST", "/api/tags", url.Values{"name": {"срочно"}}, htmx))
	var tagID string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tags WHERE name = 'срочно'`).Scan(&tagID)
	for _, c := range []struct{ method, path string }{
		{"DELETE", "/api/tags?id=" + tagID},
		{"POST", "/api/goals"},
		{"DELETE", "/api/goals?activity=вёрстка&period=daily"},
	} {
		resp = dev.do(c.method, c.path, url.Values{"activity": {"вёрстка"}, "period": {"daily"}, "minutes": {"60"}}, htmx)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("member %s %s: %d, want 403", c.method, c.path, resp.StatusCode)
		}
	}
	resp = adm.do("DELETE", "/api/tags?id="+tagID, nil, htmx)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("admin can't delete a tag: %d", resp.StatusCode)
	}

	// Client rates are for managers.
	for _, path := range []string{"/api/projects", "/api/v1/projects"} {
		if body := readBody(t, dev.do("GET", path, nil, nil)); strings.Contains(body, "billable_rate_cents") {
			t.Errorf("member sees client rates in %s", path)
		}
		if body := readBody(t, adm.do("GET", path, nil, nil)); !strings.Contains(body, "billable_rate_cents") {
			t.Errorf("admin lost client rates in %s", path)
		}
	}
	var slug string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT slug FROM projects WHERE id = ?`, pa).Scan(&slug)
	if page := readBody(t, dev.do("GET", "/projects/"+slug, nil, nil)); strings.Contains(page, `name="rate"`) {
		t.Error("member sees the rate form on the project page")
	}

	// The owner is above the admins.
	var ownerID string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'own@roles.test'`).Scan(&ownerID)
	resp = adm.do("POST", "/api/member/pay", url.Values{"user_id": {ownerID}, "hourly_pay": {"1"}}, nil)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "forbidden") {
		t.Errorf("admin changed the owner's pay: %q", resp.Header.Get("Location"))
	}
	resp = adm.do("POST", "/api/team/members/"+ownerID+"/remove", nil, nil)
	resp.Body.Close()
	var n int
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM memberships m JOIN teams t ON t.id = m.team_id WHERE m.user_id = ? AND t.name = 'Студия'`, ownerID).Scan(&n)
	if n != 1 {
		t.Fatal("admin removed the owner")
	}

	// Handing over: only the owner, the old owner stays as admin.
	resp = adm.do("POST", "/api/team/transfer", url.Values{"user_id": {admID}}, nil)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "forbidden") {
		t.Errorf("admin took the workspace: %q", resp.Header.Get("Location"))
	}
	resp = owner.do("POST", "/api/team/transfer", url.Values{"user_id": {devID}}, nil)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "transferred") {
		t.Fatalf("transfer: %q", resp.Header.Get("Location"))
	}
	roles := map[string]string{}
	rows, _ := owner.db.TestSQL().QueryContext(t.Context(), `SELECT m.user_id, m.role FROM memberships m JOIN teams t ON t.id = m.team_id WHERE t.name = 'Студия'`)
	for rows.Next() {
		var id, role string
		rows.Scan(&id, &role)
		roles[id] = role
	}
	rows.Close()
	var teamOwner string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT owner_id FROM teams WHERE name = 'Студия'`).Scan(&teamOwner)
	if roles[devID] != "owner" || roles[ownerID] != "admin" || teamOwner != devID {
		t.Errorf("after transfer: roles %v, team owner %s", roles, teamOwner)
	}
	if page := readBody(t, dev.do("GET", "/settings/members", nil, nil)); !reactData[settingsPageData](t, page).IsOwner {
		t.Error("the new owner has no transfer form")
	}
}

func TestPersonalWorkspaceNotHandedOver(t *testing.T) {
	a := newAPIEnv(t)
	a.register("solo@roles.test")
	resp := a.do("POST", "/api/team/transfer", url.Values{"user_id": {"999"}}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "transferred") {
		t.Fatalf("personal workspace handed over: %q", loc)
	}
}
