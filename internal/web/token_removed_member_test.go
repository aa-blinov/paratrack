package web

import (
	"net/url"
	"regexp"
	"strconv"
	"testing"
)

// A token minted by a member belongs to the team it was minted in. Once the
// member is removed from that team, the token must stop reading its data.
func TestAPITokenOfRemovedMemberStopsReadingTheTeam(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("token-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("token-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"token-member@x.test": member}, "token-member@x.test")

	var memberID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT id FROM users WHERE email = 'token-member@x.test'`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}

	resp := member.do("POST", "/api/tokens", url.Values{"name": {"left-behind"}, "read_only": {"1"}}, nil)
	page := readBody(t, resp)
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	if raw == "" {
		t.Fatal("the member's token is not shown on the page")
	}
	bearer := map[string]string{"Authorization": "Bearer " + mustUnescape(t, raw)}

	before := member.do("GET", "/api/v1/clients", nil, bearer)
	before.Body.Close()
	t.Logf("before removal: status %d", before.StatusCode)

	removed := owner.do("POST", "/api/team/members/"+strconv.FormatInt(memberID, 10)+"/remove", url.Values{}, nil)
	removed.Body.Close()
	if removed.StatusCode != 303 {
		t.Fatalf("remove member: status %d, want a redirect", removed.StatusCode)
	}

	after := member.do("GET", "/api/v1/clients", nil, bearer)
	body := readBody(t, after)
	if after.StatusCode == 200 {
		t.Fatalf("token of a removed member still reads the team: status %d, body %.200s", after.StatusCode, body)
	}
}
