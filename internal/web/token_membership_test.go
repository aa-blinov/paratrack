package web

import (
	"net/http"
	"testing"
)

// A genuine token whose owner has lost their seat in the token's workspace is
// answered 403, not 401: logging in again cannot help, and the CLI prompt
// tells the two apart ("access?" against "auth?").
func TestTokenWithoutMembershipIsForbiddenNotUnauthorized(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("token-seat-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("token-seat-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"token-seat-member@x.test": member}, "token-seat-member@x.test")
	auth := map[string]string{"Authorization": "Bearer " + apiTokenOf(t, member, "seat")}

	if resp := member.do("GET", "/api/active", nil, auth); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("a member's token: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if _, err := owner.db.TestSQL().ExecContext(t.Context(),
		`DELETE FROM memberships WHERE user_id = (SELECT id FROM users WHERE email = 'token-seat-member@x.test')`); err != nil {
		t.Fatal(err)
	}
	resp := member.do("GET", "/api/active", nil, auth)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a token whose owner left the workspace: %d, want 403", resp.StatusCode)
	}
	bogus := member.do("GET", "/api/active", nil, map[string]string{"Authorization": "Bearer pt_not_a_token"})
	bogus.Body.Close()
	if bogus.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an unknown token: %d, want 401", bogus.StatusCode)
	}
}
