package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// Server tests inspect the data delivered to React, rather than the retired
// Go page markup. Browser tests cover the controls rendered from this data.
func reactData[T any](t *testing.T, html string) T {
	t.Helper()
	_, rest, ok := strings.Cut(html, `<div id="react-page-data" hidden>`)
	if !ok {
		t.Fatal("missing React bootstrap")
	}
	payload, _, ok := strings.Cut(rest, "</div>")
	if !ok {
		t.Fatal("unterminated React bootstrap")
	}
	var bootstrap struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &bootstrap); err != nil {
		t.Fatal(err)
	}
	return bootstrap.Data
}
