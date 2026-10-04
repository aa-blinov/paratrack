// Package netclients holds outbound HTTP clients shared by application
// composition and transport adapters.
package netclients

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpurl"
	"github.com/aa-blinov/paratrack/internal/netpolicy"
)

// External is used for provider and identity endpoints with bounded timeouts.
// Redirects may stay within an origin, but cannot forward credentials to a
// different host supplied by a remote API response. Its cloned transport has
// an independent idle-connection pool owned by this client.
func External() *http.Client {
	return &http.Client{
		Transport: NewExternalTransport(),
		Timeout:   15 * time.Second,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if len(previous) > 0 && !httpurl.SameOrigin(previous[len(previous)-1].URL, request.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// NewExternalTransport returns an independently owned transport. It clones
// the process default when possible; arbitrary RoundTrippers cannot be cloned,
// so a custom process default falls back to equivalent standard defaults.
func NewExternalTransport() *http.Transport {
	var transport *http.Transport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	} else {
		// A custom global RoundTripper cannot be cloned safely. Keep this
		// client independently owned and retain the standard transport defaults.
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	return transport
}

// Providers is the credential-bearing client used for third-party APIs and
// manager-configured Jira/GitLab sites. It bypasses ambient proxies and checks
// the resolved dial address so a custom provider URL cannot reach local or
// reserved networks unless the operator explicitly opts in.
func Providers(allowPrivate bool) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
				Control: func(_, address string, _ syscall.RawConn) error {
					return validateProviderAddress(address, allowPrivate)
				},
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          50,
			MaxIdleConnsPerHost:   5,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if len(previous) > 0 && !httpurl.SameOrigin(previous[len(previous)-1].URL, request.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func validateProviderAddress(address string, allowPrivate bool) error {
	if allowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !netpolicy.IsPublicIP(ip) {
		return netpolicy.ErrPrivateTarget
	}
	return nil
}

// Webhook restricts outbound connections to public IPs and does not follow
// redirects, preventing webhooks from probing private network services.
func Webhook(allowPrivate bool) *http.Client {
	return newPublicEndpointClient(allowPrivate)
}

// PublicOutbound restricts server-originated requests to public IPs and does
// not follow redirects. Use it for endpoints that have no private-network
// opt-in, such as stored Web Push subscriptions.
func PublicOutbound() *http.Client {
	return newPublicEndpointClient(false)
}

func newPublicEndpointClient(allowPrivate bool) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			// Do not route webhook requests through an environment proxy: the
			// proxy would resolve the target itself and bypass DialContext's
			// public-IP check for the configured destination.
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
				Control: func(_, address string, _ syscall.RawConn) error {
					if allowPrivate {
						return nil
					}
					host, _, err := net.SplitHostPort(address)
					if err != nil {
						return err
					}
					if ip := net.ParseIP(host); ip == nil || !netpolicy.IsPublicIP(ip) {
						return netpolicy.ErrPrivateTarget
					}
					return nil
				},
			}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
