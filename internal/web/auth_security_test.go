package web

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/app"
)

// The signup confirmation (SEC-01) and the second factor of the sign-in (SEC-02). The
// scenarios run against the real database and the real HTTP routes; the mail is captured
// by a spy and the codes are computed here, from the secret the set-up returned.

func verifyOn(cfg *app.Config) { cfg.RequireEmailVerification = true }

// mailToken reads the one-time token out of the mail at index (negative counts from the end).
func mailToken(t *testing.T, spy *spyMail, index int) string {
	t.Helper()
	if index < 0 {
		index += len(spy.sent)
	}
	if index < 0 || index >= len(spy.sent) {
		t.Fatalf("no mail at %d (sent %d)", index, len(spy.sent))
	}
	body := spy.sent[index].body
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in mail body: %q", body)
	}
	return strings.TrimSpace(strings.SplitN(body[i+len("token="):], "\n", 2)[0])
}

// totpAt is the six-digit code an authenticator app shows at a moment, for a base32 secret.
func totpAt(t *testing.T, encoded string, at time.Time) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(encoded))
	if err != nil {
		t.Fatalf("secret is not base32: %v", err)
	}
	mac := hmac.New(sha1.New, secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30))
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

func location(resp *http.Response) string {
	resp.Body.Close()
	return resp.Header.Get("Location")
}

func TestSignupWaitsForItsConfirmationLink(t *testing.T) {
	e := newAPIEnvWith(t, verifyOn)
	spy := &spyMail{}
	e.srv.runtime.Mailer = spy
	anon := &apiEnv{t: t, ts: e.ts, srv: e.srv, db: e.db, jar: map[string]string{}}
	const email = "verify@x.test"
	const password = "longenough"

	if got := location(anon.do("POST", "/api/register", url.Values{"name": {"Анна"}, "email": {email}, "password": {password}}, nil)); got != "/login?notice=verify_sent" {
		t.Fatalf("signup redirected to %q, want the notice to confirm the mail", got)
	}
	if _, ok := anon.jar[sessionCookieName]; ok {
		t.Fatal("signup started a session before the address was confirmed")
	}
	if len(spy.sent) != 1 {
		t.Fatalf("signup sent %d mails, want the confirmation link", len(spy.sent))
	}
	token := mailToken(t, spy, 0)

	// Signing in before the link is opened is refused, and the link is not sent again within a minute.
	if got := location(anon.do("POST", "/api/login", url.Values{"email": {email}, "password": {password}}, nil)); got != "/login?error=email_unverified" {
		t.Fatalf("unconfirmed sign-in redirected to %q", got)
	}
	if len(spy.sent) != 1 {
		t.Fatalf("a second link went out within a minute: %d mails", len(spy.sent))
	}

	if got := location(anon.do("GET", "/verify-email?token=not-a-token", nil, nil)); got != "/login?error=verify_invalid" {
		t.Fatalf("wrong link redirected to %q", got)
	}
	if got := location(anon.do("GET", "/verify-email?token="+url.QueryEscape(token), nil, nil)); got != "/login?notice=verified" {
		t.Fatalf("right link redirected to %q", got)
	}
	if got := location(anon.do("GET", "/verify-email?token="+url.QueryEscape(token), nil, nil)); got != "/login?error=verify_invalid" {
		t.Fatalf("a used link redirected to %q, want it refused", got)
	}

	resp := anon.do("POST", "/api/login", url.Values{"email": {email}, "password": {password}}, nil)
	if got := location(resp); got != "/" && got != "/welcome" {
		t.Fatalf("confirmed sign-in redirected to %q", got)
	}
	if _, ok := anon.jar[sessionCookieName]; !ok {
		t.Fatal("confirmed sign-in set no session")
	}
}

func TestTwoFactorSignInNeedsTheCodeFromTheApp(t *testing.T) {
	e := newAPIEnv(t)
	const email = "twofactor@x.test"
	const password = "longenoughpw" // the password the register helper sets
	e.register(email)

	var setup struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	resp := e.postJSON("/api/profile/2fa/setup", map[string]any{})
	status := resp.StatusCode
	decodeBody(t, resp, &setup)
	if status != http.StatusOK || setup.Secret == "" || !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatalf("set-up = status %d, %+v", status, setup)
	}

	now := time.Now().UTC()
	code := totpAt(t, setup.Secret, now)
	var enabled struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	resp = e.postJSON("/api/profile/2fa/enable", map[string]any{"code": code})
	status = resp.StatusCode
	decodeBody(t, resp, &enabled)
	if status != http.StatusOK || len(enabled.RecoveryCodes) != 10 {
		t.Fatalf("enable = status %d, %d recovery codes", status, len(enabled.RecoveryCodes))
	}

	// The password alone no longer opens a session: the step asks for the code.
	second := newAPIEnvSharedDB(t, e)
	if got := location(second.do("POST", "/api/login", url.Values{"email": {email}, "password": {password}}, nil)); got != "/login/2fa" {
		t.Fatalf("password sign-in redirected to %q, want the code step", got)
	}
	if _, ok := second.jar[sessionCookieName]; ok {
		t.Fatal("the password alone started a session")
	}
	if resp := second.do("GET", "/login/2fa", nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("code step page: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if got := location(second.do("POST", "/api/login/2fa", url.Values{"code": {"000000"}}, nil)); !strings.HasPrefix(got, "/login/2fa?error=bad_code") {
		t.Fatalf("wrong code redirected to %q", got)
	}
	// The code of the step the set-up already used cannot sign in again.
	if got := location(second.do("POST", "/api/login/2fa", url.Values{"code": {code}}, nil)); !strings.HasPrefix(got, "/login/2fa?error=bad_code") {
		t.Fatalf("a used step's code redirected to %q, want it refused", got)
	}
	// The code of the next step signs in.
	next := totpAt(t, setup.Secret, now.Add(30*time.Second))
	if got := location(second.do("POST", "/api/login/2fa", url.Values{"code": {next}}, nil)); got != "/" && got != "/welcome" {
		t.Fatalf("next step's code redirected to %q", got)
	}
	if _, ok := second.jar[sessionCookieName]; !ok {
		t.Fatal("the right code set no session")
	}
	if got := location(second.do("POST", "/api/login/2fa", url.Values{"code": {next}}, nil)); got != "/login?error=challenge_invalid" {
		t.Fatalf("the spent step redirected to %q, want it gone", got)
	}

	// A recovery code signs in once.
	third := newAPIEnvSharedDB(t, e)
	location(third.do("POST", "/api/login", url.Values{"email": {email}, "password": {password}}, nil))
	if got := location(third.do("POST", "/api/login/2fa", url.Values{"code": {enabled.RecoveryCodes[0]}}, nil)); got != "/" && got != "/welcome" {
		t.Fatalf("recovery code redirected to %q", got)
	}
	fourth := newAPIEnvSharedDB(t, e)
	location(fourth.do("POST", "/api/login", url.Values{"email": {email}, "password": {password}}, nil))
	if got := location(fourth.do("POST", "/api/login/2fa", url.Values{"code": {enabled.RecoveryCodes[0]}}, nil)); !strings.HasPrefix(got, "/login/2fa?error=bad_code") {
		t.Fatalf("a spent recovery code redirected to %q, want it refused", got)
	}
}

func TestAnAPITokenCannotChangeTheSecondFactor(t *testing.T) {
	e := newAPIEnv(t)
	e.register("bearer-two-factor@x.test")
	token := apiTokenOf(t, e, "agent")
	resp := e.do("POST", "/api/profile/2fa/setup", nil, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a bearer token set up the second factor: status %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPasswordResetAlsoConfirmsTheAddress(t *testing.T) {
	e := newAPIEnvWith(t, verifyOn)
	spy := &spyMail{}
	e.srv.runtime.Mailer = spy
	anon := &apiEnv{t: t, ts: e.ts, srv: e.srv, db: e.db, jar: map[string]string{}}
	const email = "reset-verify@x.test"

	location(anon.do("POST", "/api/register", url.Values{"name": {"Олег"}, "email": {email}, "password": {"longenough"}}, nil))
	location(anon.do("POST", "/api/password/forgot", url.Values{"email": {email}}, nil))
	if len(spy.sent) != 2 {
		t.Fatalf("mails sent = %d, want the confirmation and the reset", len(spy.sent))
	}
	resetToken := mailToken(t, spy, -1)
	if got := location(anon.do("POST", "/api/password/reset", url.Values{"token": {resetToken}, "new_password": {"brandnewpw1"}}, nil)); got == "" {
		t.Fatal("the reset was not accepted")
	}
	resp := anon.do("POST", "/api/login", url.Values{"email": {email}, "password": {"brandnewpw1"}}, nil)
	if got := location(resp); got != "/" && got != "/welcome" {
		t.Fatalf("sign-in after the reset redirected to %q: the address was not confirmed by the reset", got)
	}
}
