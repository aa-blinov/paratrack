package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A run that pays hours already paid is refused without a reason, even when the manager
// confirms it: the answer sends them back with the reason asked for (FIN-22).
func TestConfirmedOverlapNeedsAReason(t *testing.T) {
	e := newAPIEnv(t)
	e.register("overlap-reason@x.test")
	resp := e.do("POST", "/payroll", url.Values{
		"start": {"2026-10-01"}, "end": {"2026-10-07"}, "notes": {""}, "confirm": {"1"}, "overlap_reason": {""},
	}, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=") {
		t.Fatalf("a confirmed overlap without a reason: status %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if strings.Contains(resp.Header.Get("Location"), "/payroll/") {
		t.Fatalf("a run was created without a reason: %q", resp.Header.Get("Location"))
	}
}
