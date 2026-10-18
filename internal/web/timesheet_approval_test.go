package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Only a manager approves or reopens a week. A member is refused by the route
// itself; the owner gets back to the timesheet on the approved week.
func TestTimesheetApprovalIsManagerOnly(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("owner-approval@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("member-approval@x.test")

	resp := owner.do("POST", "/api/team/invites", nil, nil)
	loc, _ := url.QueryUnescape(resp.Header.Get("Location"))
	resp.Body.Close()
	token := regexp.MustCompile(`/invites/([A-Za-z0-9_-]+)`).FindStringSubmatch(loc)
	if token == nil {
		t.Fatalf("no invite link in %q", loc)
	}
	resp = member.do("POST", "/api/invites/"+token[1]+"/accept", nil, nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		member.jar[c.Name] = c.Value
	}

	// A past week, so it has ended and can be approved.
	form := url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}
	refused := member.do("POST", "/api/timesheet/approve", form, nil)
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("member approve: status %d, want 403", refused.StatusCode)
	}

	approved := owner.do("POST", "/api/timesheet/approve", form, nil)
	approved.Body.Close()
	if approved.StatusCode != http.StatusSeeOther || !strings.HasPrefix(approved.Header.Get("Location"), "/timesheet?date=2020-03-02") {
		t.Fatalf("owner approve: status %d to %q", approved.StatusCode, approved.Header.Get("Location"))
	}

	reopened := owner.do("POST", "/api/timesheet/reopen", form, nil)
	reopened.Body.Close()
	if reopened.StatusCode != http.StatusSeeOther || !strings.HasPrefix(reopened.Header.Get("Location"), "/timesheet?date=2020-03-02") {
		t.Fatalf("owner reopen: status %d to %q", reopened.StatusCode, reopened.Header.Get("Location"))
	}
}
