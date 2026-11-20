package web

import (
	"net/http"
	"net/url"
	"testing"
)

// The public auth routes carry the budgets of PRODUCT.md §3, and the login
// budget is covered in saas_test.go. This file holds the rest to the same
// standard: a budget that only exists in the route table can silently
// disappear in a refactor, exhaustion must answer 429.

// Registration is throttled per mailbox: ten attempts on one address, then
// the eleventh waits for the minute to pass, whatever IP it comes from.
func TestRateLimitRegisterMailbox(t *testing.T) {
	e := newAPIEnv(t)
	code := 0
	for i := 0; i < 11; i++ {
		resp := e.do("POST", "/api/register", url.Values{"name": {"R"}, "email": {"same-mailbox@x.test"}, "password": {"longenoughpw"}}, nil)
		code = resp.StatusCode
		resp.Body.Close()
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh registration with one mailbox = %d, want 429", code)
	}
}

// The forgot-password letter is the most abuse-prone public send: five per
// minute from an address, then a refusal.
func TestRateLimitPasswordForgot(t *testing.T) {
	e := newAPIEnv(t)
	code := 0
	for i := 0; i < 6; i++ {
		resp := e.do("POST", "/api/password/forgot", url.Values{"email": {"who@x.test"}}, nil)
		code = resp.StatusCode
		resp.Body.Close()
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("the sixth forgot-password = %d, want 429", code)
	}
}

// Reset posts are budgeted before the token is validated: a guesser gets
// ten tries a minute, valid tokens or not.
func TestRateLimitPasswordReset(t *testing.T) {
	e := newAPIEnv(t)
	code := 0
	for i := 0; i < 11; i++ {
		resp := e.do("POST", "/api/password/reset", url.Values{
			"token": {"no-such-token"}, "new_password": {"againpw1234"},
		}, nil)
		code = resp.StatusCode
		resp.Body.Close()
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh reset attempt = %d, want 429", code)
	}
}
