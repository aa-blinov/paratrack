package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// OIDC SSO (optional, env-configured)
// ---------------------------------------------------------------------------

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func pkceS256(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// handleSSOLogin redirects to the OIDC provider (authorization endpoint).
// Configured via PARATRACK_OIDC_ISSUER / _CLIENT_ID / _CLIENT_SECRET.
func (s *Server) handleSSOLogin(w http.ResponseWriter, r *http.Request) {
	if !s.config.OIDCEnabled {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	state, err := randomToken(16)
	if err != nil {
		s.logger.Printf("sso: %v", err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	codeVerifier, err := randomToken(32)
	if err != nil {
		s.logger.Printf("sso: %v", err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	nonce, err := randomToken(16)
	if err != nil {
		s.logger.Printf("sso: %v", err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_state", Value: state, Path: "/sso", HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_nonce", Value: nonce, Path: "/sso", HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_pkce", Value: codeVerifier, Path: "/sso", HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	authorizationURL, err := s.runtime.OIDC.AuthorizationURL(r.Context(), OIDCAuthorizationRequest{
		RedirectURI: s.publicBaseURL(r) + "/sso/callback",
		State:       state, Nonce: nonce, Challenge: pkceS256(codeVerifier),
	})
	if err != nil {
		s.logger.Printf("sso: %v", err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

// handleSSOCallback exchanges the code, resolves the email, and logs
// the user in (creating an account on first use). An existing account is
// joined by email only when the provider vouches for that email
// (email_verified), or the operator says it always does.
func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(code, why string) {
		if why != "" {
			s.logger.Printf("sso: %s", why)
		}
		http.Redirect(w, r, "/login?error="+code, http.StatusSeeOther)
	}
	if !s.config.OIDCEnabled {
		fail("internal", "SSO is not configured")
		return
	}
	q := r.URL.Query()
	st, err := r.Cookie("paratrack_oidc_state")
	if err != nil || st.Value == "" || subtle.ConstantTimeCompare([]byte(st.Value), []byte(q.Get("state"))) != 1 {
		fail("internal", "state mismatch")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_state", Value: "", Path: "/sso", MaxAge: -1})
	nonceCookie, err := r.Cookie("paratrack_oidc_nonce")
	if err != nil || nonceCookie.Value == "" {
		fail("internal", "nonce missing")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_nonce", Value: "", Path: "/sso", MaxAge: -1})
	pkceCookie, err := r.Cookie("paratrack_oidc_pkce")
	if err != nil || pkceCookie.Value == "" {
		fail("internal", "PKCE verifier missing")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_pkce", Value: "", Path: "/sso", MaxAge: -1})
	code := q.Get("code")
	if code == "" {
		fail("internal", "no code")
		return
	}
	identity, err := s.runtime.OIDC.Authenticate(r.Context(), OIDCAuthenticationRequest{
		Code: code, Verifier: pkceCookie.Value, Nonce: nonceCookie.Value,
		RedirectURI: s.publicBaseURL(r) + "/sso/callback",
	})
	if err != nil {
		fail("internal", err.Error())
		return
	}
	if !identity.EmailVerified {
		// Without it anyone controlling an IdP account could claim an
		// existing paratrack account by its email.
		fail("bad_email", "email not verified by the provider")
		return
	}
	name := identity.Name
	if name == "" {
		name = strings.SplitN(identity.Email, "@", 2)[0]
	}
	teamName := localizedPersonalTeamName(resolveLang(r), name)
	account, sess, _, err := s.services.Auth.SignIn.AuthenticateSSO(operationContext(r), appmodel.SSOAuthenticationRequest{
		Email: identity.Email, Name: name, TeamName: teamName, Subject: identity.Subject,
	})
	if err != nil {
		if errors.Is(err, appmodel.ErrAuthInvalidEmail) || errors.Is(err, appmodel.ErrAuthValidation) {
			http.Redirect(w, r, "/register?error=could_not_register", http.StatusSeeOther)
			return
		}
		fail("internal", err.Error())
		return
	}
	setSessionCookie(w, r, sess.Token)
	// An SSO sign-in is a sign-in: it returns to the last workspace, and
	// a first-time SSO account has none to return to.
	s.restoreLastWorkspace(w, r, account.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
