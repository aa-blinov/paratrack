package web

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type trustedProxiesKey struct{}
type securePublicOriginKey struct{}

func withTrustedProxies(next http.Handler, prefixes []netip.Prefix) http.Handler {
	configured := append([]netip.Prefix(nil), prefixes...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), trustedProxiesKey{}, configured)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withSecurePublicOrigin marks requests secure when the process's configured
// canonical origin is HTTPS, including TLS-terminated deployments where the
// trusted proxy list is intentionally limited to forwarded-header handling.
func withSecurePublicOrigin(next http.Handler, origin string) http.Handler {
	parsed, err := url.Parse(origin)
	secure := err == nil && strings.EqualFold(parsed.Scheme, "https") && parsed.Host != "" && parsed.User == nil
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), securePublicOriginKey{}, secure)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func trustedProxyPrefixes(r *http.Request) []netip.Prefix {
	if r == nil {
		return nil
	}
	prefixes, _ := r.Context().Value(trustedProxiesKey{}).([]netip.Prefix)
	remote, ok := remoteIP(r.RemoteAddr)
	if !ok || !containsIP(prefixes, remote) {
		return nil
	}
	return prefixes
}

func containsIP(prefixes []netip.Prefix, address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func remoteIP(remoteAddr string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	address, err := netip.ParseAddr(remoteAddr)
	return address.Unmap(), err == nil
}

func forwardedIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap(), true
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		address, err := netip.ParseAddr(host)
		return address.Unmap(), err == nil
	}
	return netip.Addr{}, false
}

// clientIP extracts the caller's address. Forwarded addresses are consulted
// only when the immediate peer belongs to the configured trusted proxy ranges.
func clientIP(r *http.Request) string {
	if prefixes := trustedProxyPrefixes(r); len(prefixes) > 0 {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				address, ok := forwardedIP(parts[i])
				if !ok {
					break
				}
				if !containsIP(prefixes, address) {
					return address.String()
				}
			}
		}
	}
	if address, ok := remoteIP(r.RemoteAddr); ok {
		return address.String()
	}
	return r.RemoteAddr
}

func operationContext(r *http.Request) context.Context {
	ctx := requestctx.WithClientIP(r.Context(), clientIP(r))
	return requestctx.WithTeamID(ctx, teamID(r))
}
