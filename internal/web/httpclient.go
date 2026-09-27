package web

import (
	"errors"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"
)

// extClient calls third-party APIs (task and history imports, OIDC).
// http.DefaultClient has no timeout: one slow provider hung the request.
var extClient = &http.Client{Timeout: 15 * time.Second}

// hookClient delivers outbound webhooks to user-supplied URLs. It refuses
// loopback, private and link-local targets at dial time (so DNS rebinding
// can't sneak past a name check) and doesn't follow redirects: a webhook
// must not become a way to probe the server's own network. Self-hosters
// posting to a LAN service set PARATRACK_WEBHOOK_ALLOW_PRIVATE=1.
var hookClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				if os.Getenv("PARATRACK_WEBHOOK_ALLOW_PRIVATE") == "1" {
					return nil
				}
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
					return errPrivateTarget
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

var errPrivateTarget = errors.New("webhook target is not a public address")

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast())
}
