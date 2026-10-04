package web

import (
	"net/http"
	"strings"
)

// publicBaseURL reconstructs scheme://host for links sent to users. Behind
// TLS it honours forwarded host and protocol only from trusted proxies.
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	if host := r.Header.Get("X-Forwarded-Host"); len(trustedProxyPrefixes(r)) > 0 && host != "" {
		return scheme + "://" + strings.TrimSpace(host)
	}
	return scheme + "://" + r.Host
}

func (s *Server) publicBaseURL(r *http.Request) string {
	if s.config.PublicURL != "" {
		return s.config.PublicURL
	}
	return publicBaseURL(r)
}
