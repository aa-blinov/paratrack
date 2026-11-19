// The CSRF cookie is named for the transport: `__Host-paratrack_csrf` where
// the page is HTTPS — the prefix pins the cookie to the host — and the plain
// `paratrack_csrf` on local HTTP, where the browser forbids the prefix. The
// server sets the one it can; every client read tries the pinned name first
// and falls back to the plain one, so the same build works on both.
const CSRF_COOKIE_NAMES = ["__Host-paratrack_csrf", "paratrack_csrf"]

export function readCSRFCookie(): string {
  for (const name of CSRF_COOKIE_NAMES) {
    const match = document.cookie.match(new RegExp(`(?:^|;\\s*)${name}=([^;]*)`))
    if (!match) continue
    try { return decodeURIComponent(match[1]) } catch { return match[1] }
  }
  return ""
}
