package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func forwardedRequest(request *http.Request, prefixes ...netip.Prefix) *http.Request {
	var forwarded *http.Request
	withTrustedProxies(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		forwarded = r
	}), prefixes).ServeHTTP(httptest.NewRecorder(), request)
	return forwarded
}

func TestForwardedHeadersRequireTrustedImmediatePeer(t *testing.T) {
	prefix := netip.MustParsePrefix("10.0.0.0/8")
	direct := httptest.NewRequest("GET", "http://paratrack.test/", nil)
	direct.RemoteAddr = "198.51.100.8:43000"
	direct.Header.Set("X-Forwarded-Proto", "https")
	direct.Header.Set("X-Forwarded-For", "203.0.113.55")
	request := forwardedRequest(direct, prefix)
	if isSecureRequest(request) {
		t.Fatal("direct peer spoofed a secure forwarded protocol")
	}
	if got := clientIP(request); got != "198.51.100.8" {
		t.Fatalf("direct peer spoofed client IP: got %q", got)
	}
}

func TestTrustedProxyUsesRightmostUntrustedForwardedAddress(t *testing.T) {
	prefixes := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}
	req := httptest.NewRequest("GET", "http://paratrack.test/", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-For", "203.0.113.55, 198.51.100.9, 192.0.2.7")
	request := forwardedRequest(req, prefixes...)
	if !isSecureRequest(request) {
		t.Fatal("trusted TLS proxy was not recognized")
	}
	if got := clientIP(request); got != "198.51.100.9" {
		t.Fatalf("client IP = %q, want nearest untrusted address 198.51.100.9", got)
	}
}

func TestSecureCanonicalOriginSetsSecureRequestContextWithoutProxyHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "http://paratrack-internal:8888/", nil)
	req.RemoteAddr = "10.0.0.2:41000"
	var wrapped *http.Request
	withSecurePublicOrigin(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapped = r
	}), "https://app.example.com").ServeHTTP(httptest.NewRecorder(), req)
	if !isSecureRequest(wrapped) {
		t.Fatal("configured HTTPS public origin did not mark the request secure")
	}
}

func TestHTTPPublicOriginDoesNotOverrideUntrustedForwardedProtocol(t *testing.T) {
	req := httptest.NewRequest("GET", "http://paratrack-internal:8888/", nil)
	req.RemoteAddr = "10.0.0.2:41000"
	req.Header.Set("X-Forwarded-Proto", "https")
	var wrapped *http.Request
	withTrustedProxies(withSecurePublicOrigin(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapped = r
	}), "http://app.example.com"), nil).ServeHTTP(httptest.NewRecorder(), req)
	if isSecureRequest(wrapped) {
		t.Fatal("HTTP public origin trusted an unconfigured forwarded protocol")
	}
}
