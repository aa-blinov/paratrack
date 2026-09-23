package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// urlEscape is a tiny wrapper around url.QueryEscape that also strips
// newlines so the value is safe to put in a Location header.
func urlEscape(s string) string {
	return url.QueryEscape(strings.ReplaceAll(s, "\n", ""))
}

// intToString is a one-liner used in cookie values; strconv.FormatInt
// is fine but a typed helper reads better at the call site.
func intToString(n int64) string { return strconv.FormatInt(n, 10) }

// scanInt parses s as a base-10 int64. Returns the number of bytes
// consumed (always 0 for empty / non-numeric input).
func scanInt(s string, dst *int64) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	*dst = v
	return len(s), nil
}