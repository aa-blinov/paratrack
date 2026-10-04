// Package httpurl validates URLs used by outbound HTTP adapters.
package httpurl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("URL is outside the trusted HTTP origin")

// SameOrigin reports whether two absolute HTTP(S) URLs share an origin.
// Userinfo and fragments are rejected so credentials cannot hide in a URL.
func SameOrigin(left, right *url.URL) bool {
	if !validURL(left) || !validURL(right) || !strings.EqualFold(left.Scheme, right.Scheme) {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(left.Hostname(), "."), strings.TrimSuffix(right.Hostname(), ".")) && effectivePort(left) == effectivePort(right)
}

// ResolveSameOrigin resolves a pagination reference and verifies that it
// remains on the base URL's HTTP origin before credentials are attached.
func ResolveSameOrigin(base, reference string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil || !validURL(baseURL) {
		return "", fmt.Errorf("%w: invalid base URL", ErrUnsafeURL)
	}
	referenceURL, err := url.Parse(reference)
	if err != nil {
		return "", fmt.Errorf("%w: invalid pagination reference", ErrUnsafeURL)
	}
	resolved := baseURL.ResolveReference(referenceURL)
	if !SameOrigin(baseURL, resolved) {
		return "", fmt.Errorf("%w: pagination reference changes origin", ErrUnsafeURL)
	}
	return resolved.String(), nil
}

func validURL(value *url.URL) bool {
	return value != nil && (strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https")) &&
		value.Hostname() != "" && value.User == nil && value.Fragment == "" && value.Opaque == ""
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}
