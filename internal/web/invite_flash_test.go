package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// An invitation names the address it was meant for and says when no letter went out,
// so the owner knows to send the link by hand (ADM-18).
func TestInviteFlashSaysWhetherTheLetterWentOut(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invite-flash@x.test")

	bad := e.do("POST", "/api/team/invites", url.Values{"email": {"не почта"}}, nil)
	readBody(t, bad)
	if bad.StatusCode != http.StatusSeeOther {
		t.Fatalf("invite answered %d, want a redirect", bad.StatusCode)
	}
	if got := flashFromLocation(t, bad.Header.Get("Location")); got != i18n.T(i18n.Ru, "flash.inviteBadAddress") {
		t.Fatalf("an address that does not parse answered %q", got)
	}

	// The test server has no mailer, so a valid address is reported as not sent.
	valid := e.do("POST", "/api/team/invites", url.Values{"email": {"new.member@x.test"}}, nil)
	readBody(t, valid)
	if got := flashFromLocation(t, valid.Header.Get("Location")); !strings.Contains(got, "new.member@x.test") || got == i18n.T(i18n.Ru, "flash.inviteCreated") {
		t.Fatalf("a valid address without a mailer answered %q, want the address and that no letter went out", got)
	}
}

func flashFromLocation(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("redirect %q: %v", location, err)
	}
	if u.Path != "/settings/invites" {
		t.Fatalf("redirect went to %q, want the invitations page", u.Path)
	}
	return u.Query().Get("flash")
}
