// Package calendarsync holds the outbound adapters of the calendar sync
// workflow, one file per provider. Each speaks that provider's documented OAuth
// and calendar API; the tests check them against local stub servers only.
package calendarsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/httpjson"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

// Config is what an adapter needs from the process. Endpoints left empty mean
// the documented production URLs; the tests point them at a stub server.
type Config struct {
	HTTPClient  *http.Client
	Credentials calendarport.Credentials
	// Now is the injected clock that dates token expiry; adapters read no wall clock.
	Now       func() time.Time
	Endpoints Endpoints
}

// Endpoints overrides the provider URLs. Empty fields keep the production values.
type Endpoints struct {
	Authorization string
	Token         string
	API           string
}

// maxPages bounds one listing. A member's window is 120 days; hitting the cap
// fails the sync rather than deleting what was never read.
const maxPages = 50

// defaultTokenLifetime is used when a provider omits expires_in.
const defaultTokenLifetime = time.Hour

var errTooManyPages = errors.New("the provider returned too many pages for one window")

// base carries what every provider call needs: the vendor name for error text,
// the owned HTTP client, the clock and the OAuth client.
type base struct {
	vendor    string
	client    *http.Client
	now       func() time.Time
	creds     calendarport.Credentials
	endpoints Endpoints
}

func newBase(vendor string, cfg Config, defaults Endpoints) (base, error) {
	if cfg.HTTPClient == nil || cfg.Now == nil {
		return base{}, fmt.Errorf("%s calendar adapter needs an HTTP client and a clock", vendor)
	}
	if cfg.Credentials.ClientID == "" || cfg.Credentials.ClientSecret == "" {
		return base{}, fmt.Errorf("%s calendar adapter needs an OAuth client", vendor)
	}
	endpoints := cfg.Endpoints
	if endpoints.Authorization == "" {
		endpoints.Authorization = defaults.Authorization
	}
	if endpoints.Token == "" {
		endpoints.Token = defaults.Token
	}
	if endpoints.API == "" {
		endpoints.API = defaults.API
	}
	return base{vendor: vendor, client: cfg.HTTPClient, now: cfg.Now, creds: cfg.Credentials, endpoints: endpoints}, nil
}

// postToken exchanges a code or refreshes a token. On a refresh, 400 and 401 mean the
// stored grant is no longer accepted, which is ErrTokenRejected.
func (b base) postToken(ctx context.Context, form url.Values, refresh bool) (calendarport.Tokens, error) {
	form.Set("client_id", b.creds.ClientID)
	form.Set("client_secret", b.creds.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return calendarport.Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return calendarport.Tokens{}, fmt.Errorf("%s token endpoint: %w", b.vendor, calendarport.ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || (refresh && resp.StatusCode == http.StatusBadRequest) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return calendarport.Tokens{}, calendarport.ErrTokenRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return calendarport.Tokens{}, &providerstatus.Error{Vendor: b.vendor, StatusCode: resp.StatusCode}
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := httpjson.Decode(resp.Body, httpjson.MaxResponseBytes, &answer); err != nil {
		return calendarport.Tokens{}, fmt.Errorf("decode %s token response: %w", b.vendor, err)
	}
	if answer.AccessToken == "" {
		return calendarport.Tokens{}, fmt.Errorf("%s token response has no access token", b.vendor)
	}
	lifetime := defaultTokenLifetime
	if answer.ExpiresIn > 0 {
		lifetime = time.Duration(answer.ExpiresIn) * time.Second
	}
	return calendarport.Tokens{AccessToken: answer.AccessToken, RefreshToken: answer.RefreshToken, ExpiresAt: b.now().Add(lifetime)}, nil
}

// send makes one API call with the member's access token. A 401 is ErrTokenRejected,
// any other non-2xx answer is a providerstatus.Error that carries the status only.
// A 2xx answer is decoded into into when it is not nil.
func (b base) send(ctx context.Context, method, endpoint, token string, header http.Header, payload, into any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", b.vendor, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, values := range header {
		req.Header[name] = values
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s API: %w", b.vendor, calendarport.ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return calendarport.ErrTokenRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return &providerstatus.Error{Vendor: b.vendor, StatusCode: resp.StatusCode}
	}
	if into == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil
	}
	if err := httpjson.Decode(resp.Body, httpjson.MaxResponseBytes, into); err != nil {
		return fmt.Errorf("decode %s response: %w", b.vendor, err)
	}
	return nil
}

// isGone reports a provider answer that means the event no longer exists.
func isGone(err error) bool {
	var status *providerstatus.Error
	return errors.As(err, &status) && (status.StatusCode == http.StatusNotFound || status.StatusCode == http.StatusGone)
}
