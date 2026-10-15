package web

import "testing"

// «/\example.org» passed the old «starts with / but not //» check, and a
// browser reads the backslash as a slash: a sign-in link that left the site.
func TestLocalPathRefusesOffsiteRedirects(t *testing.T) {
	for _, next := range []string{"/", "/invites/abc", "/stats?period=week", "/settings/money#stripe"} {
		if !localPath(next) {
			t.Errorf("%q is a local path", next)
		}
	}
	for _, next := range []string{"", "//evil.example", "/\\evil.example", "/\\/evil.example", "https://evil.example", "evil.example", "/\nx", "\\\\evil.example"} {
		if localPath(next) {
			t.Errorf("%q must not be followed", next)
		}
	}
}
