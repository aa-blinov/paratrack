// Package oidcclient implements the outbound OpenID Connect protocol used by
// the HTTP sign-in adapter.
package oidcclient

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/httpjson"
	"github.com/aa-blinov/paratrack/internal/oidcport"
	oidclib "github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrIncompleteDependencies = errors.New("OIDC client dependencies are incomplete")
	ErrInvalidIssuer          = errors.New("invalid OIDC issuer")
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	TrustEmail   bool
	RequireHTTPS bool
}

type Client struct {
	httpClient  *http.Client
	config      Config
	discoveryMu sync.Mutex
	discovery   *discoveryDocument
}

func New(httpClient *http.Client, config Config) (*Client, error) {
	if depcheck.IsNil(httpClient) || strings.TrimSpace(config.Issuer) == "" || strings.TrimSpace(config.ClientID) == "" {
		return nil, ErrIncompleteDependencies
	}
	if err := validateURL(config.Issuer, config.RequireHTTPS); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidIssuer, err)
	}
	return &Client{httpClient: httpClient, config: config}, nil
}

func (c *Client) CloseIdleConnections() { c.httpClient.CloseIdleConnections() }

func (c *Client) AuthorizationURL(ctx context.Context, input oidcport.AuthorizationRequest) (string, error) {
	endpoints, err := c.discover(ctx)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(endpoints.Authorization)
	if err != nil { // discover validates every endpoint before returning it.
		return "", fmt.Errorf("parse OIDC authorization endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("client_id", c.config.ClientID)
	query.Set("response_type", "code")
	query.Set("scope", "openid email profile")
	query.Set("redirect_uri", input.RedirectURI)
	query.Set("state", input.State)
	query.Set("nonce", input.Nonce)
	query.Set("code_challenge", input.Challenge)
	query.Set("code_challenge_method", "S256")
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *Client) Authenticate(ctx context.Context, input oidcport.AuthenticationRequest) (oidcport.Identity, error) {
	var identity oidcport.Identity
	endpoints, err := c.discover(ctx)
	if err != nil {
		return identity, err
	}
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {input.Code},
		"redirect_uri":  {input.RedirectURI},
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
		"code_verifier": {input.Verifier},
	}
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoints.Token, strings.NewReader(tokenForm.Encode()))
	if err != nil {
		return identity, fmt.Errorf("create OIDC token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResp, err := c.httpClient.Do(tokenReq)
	if err != nil {
		return identity, fmt.Errorf("exchange OIDC authorization code: %w", err)
	}
	defer tokenResp.Body.Close()
	var tokens struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if tokenResp.StatusCode != http.StatusOK {
		return identity, fmt.Errorf("OIDC token endpoint returned %s", tokenResp.Status)
	}
	if err := httpjson.Decode(tokenResp.Body, httpjson.MaxResponseBytes, &tokens); err != nil {
		return identity, fmt.Errorf("decode OIDC token response: %w", err)
	}
	if tokens.AccessToken == "" || tokens.IDToken == "" {
		return identity, errors.New("OIDC token response is incomplete")
	}

	keyContext := oidclib.ClientContext(ctx, c.httpClient)
	keySet := oidclib.NewRemoteKeySet(keyContext, endpoints.JWKS)
	verifier := oidclib.NewVerifier(endpoints.Issuer, keySet, &oidclib.Config{
		ClientID: c.config.ClientID, SupportedSigningAlgs: endpoints.SigningAlgs,
	})
	idToken, err := verifier.Verify(ctx, tokens.IDToken)
	if err != nil {
		return identity, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	if idToken.Subject == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(input.Nonce)) != 1 {
		return identity, errors.New("OIDC ID token subject or nonce mismatch")
	}
	if idToken.AccessTokenHash != "" && idToken.VerifyAccessToken(tokens.AccessToken) != nil {
		return identity, errors.New("OIDC ID token access token hash mismatch")
	}

	userInfoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints.UserInfo, nil)
	if err != nil {
		return identity, fmt.Errorf("create OIDC userinfo request: %w", err)
	}
	userInfoReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	userInfoResp, err := c.httpClient.Do(userInfoReq)
	if err != nil {
		return identity, fmt.Errorf("fetch OIDC userinfo: %w", err)
	}
	defer userInfoResp.Body.Close()
	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
		Subject       string `json:"sub"`
	}
	if userInfoResp.StatusCode != http.StatusOK {
		return identity, fmt.Errorf("OIDC userinfo endpoint returned %s", userInfoResp.Status)
	}
	if err := httpjson.Decode(userInfoResp.Body, httpjson.MaxResponseBytes, &claims); err != nil {
		return identity, fmt.Errorf("decode OIDC userinfo response: %w", err)
	}
	if claims.Email == "" {
		return identity, errors.New("OIDC userinfo has no email")
	}
	if claims.Subject == "" || claims.Subject != idToken.Subject {
		return identity, errors.New("OIDC userinfo subject does not match ID token")
	}
	verified := claims.EmailVerified == true || claims.EmailVerified == "true" || c.config.TrustEmail
	return oidcport.Identity{Subject: claims.Subject, Email: claims.Email, Name: claims.Name, EmailVerified: verified}, nil
}

type discoveryDocument struct {
	Issuer        string   `json:"issuer"`
	Authorization string   `json:"authorization_endpoint"`
	Token         string   `json:"token_endpoint"`
	UserInfo      string   `json:"userinfo_endpoint"`
	JWKS          string   `json:"jwks_uri"`
	SigningAlgs   []string `json:"id_token_signing_alg_values_supported"`
}

func (c *Client) discover(ctx context.Context) (discoveryDocument, error) {
	c.discoveryMu.Lock()
	defer c.discoveryMu.Unlock()
	if c.discovery != nil {
		return *c.discovery, nil
	}
	endpoints, err := c.fetchDiscovery(ctx)
	if err != nil {
		return discoveryDocument{}, err
	}
	c.discovery = &endpoints
	return endpoints, nil
}

func (c *Client) fetchDiscovery(ctx context.Context) (discoveryDocument, error) {
	var endpoints discoveryDocument
	discoveryURL := strings.TrimSuffix(c.config.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return endpoints, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return endpoints, fmt.Errorf("fetch OIDC discovery document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return endpoints, fmt.Errorf("OIDC discovery endpoint returned %s", resp.Status)
	}
	if err := httpjson.Decode(resp.Body, httpjson.MaxResponseBytes, &endpoints); err != nil {
		return endpoints, fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if endpoints.Issuer != c.config.Issuer {
		return discoveryDocument{}, errors.New("OIDC discovery issuer does not match configured issuer")
	}
	if endpoints.Authorization == "" || endpoints.Token == "" || endpoints.UserInfo == "" || endpoints.JWKS == "" {
		return discoveryDocument{}, errors.New("OIDC discovery document has missing endpoints")
	}
	for name, endpoint := range map[string]string{
		"authorization": endpoints.Authorization,
		"token":         endpoints.Token,
		"userinfo":      endpoints.UserInfo,
		"jwks":          endpoints.JWKS,
	} {
		if err := validateURL(endpoint, c.config.RequireHTTPS); err != nil {
			return discoveryDocument{}, fmt.Errorf("OIDC discovery %s endpoint: %w", name, err)
		}
	}
	return endpoints, nil
}

func validateURL(raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("endpoint must be an absolute http(s) URL without credentials or fragment")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return errors.New("https is required in this environment")
	}
	return nil
}
