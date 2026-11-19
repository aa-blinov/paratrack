package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The recurring schedule (UX-20) is one checkbox in the money settings: the
// owner switches it on, the settings page reads it back, and the workflow
// keeps the schedule from saving anything else.
func TestRecurringInvoicesSettingRoundTrip(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("recurring-owner@x.test")

	readSettings := func() (recurring, flash string) {
		page := readBody(t, owner.do("GET", "/settings/money", nil, nil))
		data := reactData[settingsPageData](t, page)
		flash = data.Flash
		return data.Billing.Recurring, flash
	}

	// Off is the default state of the money settings.
	if recurring, _ := readSettings(); recurring != "" {
		t.Fatalf("fresh settings recurring = %q, want off", recurring)
	}
	save := owner.do("POST", "/api/team/billing", url.Values{
		"round_minutes":      {"15"},
		"round_mode":         {"nearest"},
		"invoice_prefix":     {"INV"},
		"recurring_invoices": {"1st"},
	}, nil)
	readBody(t, save)
	if recurring, _ := readSettings(); recurring != "1st" {
		t.Fatalf("recurring = %q, want the 1st-of-month schedule on", recurring)
	}

	// Unticking the box is the off state: the checkbox posts nothing.
	untick := owner.do("POST", "/api/team/billing", url.Values{
		"round_minutes":  {"15"},
		"round_mode":     {"nearest"},
		"invoice_prefix": {"INV"},
	}, nil)
	readBody(t, untick)
	if recurring, _ := readSettings(); recurring != "" {
		t.Fatalf("recurring after untick = %q, want off", recurring)
	}

	// An unknown schedule name does not exist: the workflow names the refusal
	// instead of saving it.
	daily := owner.do("POST", "/api/team/billing", url.Values{
		"round_minutes":      {"15"},
		"round_mode":         {"nearest"},
		"invoice_prefix":     {"INV"},
		"recurring_invoices": {"daily"},
	}, nil)
	data := reactData[settingsPageData](t, readBody(t, owner.do("GET", redirectOf(t, daily), nil, nil)))
	// The interface is Russian; the refusal names the invoice rules whatever
	// the language answers in.
	flash := data.Flash
	if !strings.Contains(flash, "правила счёта") && !strings.Contains(flash, "invoice rules") {
		t.Fatalf("refusal = %q, want the invoice rules named", flash)
	}
	if recurring := data.Billing.Recurring; recurring != "" {
		t.Fatalf("recurring after the refusal = %q, want still off", recurring)
	}
}

// redirectOf walks the redirect the mutating calls answer with: the page the
// flash lives on, its location stripped of the browser-only fragment.
func redirectOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("the call did not redirect")
	}
	if cut, _, ok := strings.Cut(location, "#"); ok {
		return cut
	}
	return location
}
