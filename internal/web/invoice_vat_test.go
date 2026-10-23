package web

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// The VAT inside a price that includes it is worked out to the cent, half up (FIN-16).
func TestVATInsideAPriceIsWorkedOutHalfUp(t *testing.T) {
	cases := []struct{ total, rate, want int }{
		{12000, 20, 2000},
		{10000, 10, 909},
		{10000, 0, 0},
		{10000, -5, 0},
	}
	for _, c := range cases {
		if got := vatIncludedCents(c.total, c.rate); got != c.want {
			t.Errorf("vatIncludedCents(%d, %d) = %d, want %d", c.total, c.rate, got, c.want)
		}
	}
}

// A rate set in the requisites shows on the invoice drafted after it, and an invoice keeps the
// rate it was drafted with when the workspace rate changes later (FIN-16).
func TestInvoiceKeepsTheVATRateItWasDraftedWith(t *testing.T) {
	e := newAPIEnv(t)
	e.register("vat@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"НДС"}, "rate": {"3000"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}, "project_id": {"1"}}, htmx))
	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП"}, "vat_rate": {"20"}}, nil))

	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	created := e.do("POST", "/invoices", url.Values{"confirm_unapproved": {"1"}, "project_id": {"1"}, "client": {"Клиент"}, "start": {day}, "end": {day}}, nil)
	readBody(t, created)
	loc := created.Header.Get("Location")
	if !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("the invoice was not drafted: %q", loc)
	}
	page := readBody(t, e.do("GET", loc, nil, nil))
	if !strings.Contains(page, "НДС 20%") {
		t.Fatal("the invoice does not say how much VAT is inside its total")
	}

	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП"}, "vat_rate": {"10"}}, nil))
	again := readBody(t, e.do("GET", loc, nil, nil))
	if !strings.Contains(again, "НДС 20%") || strings.Contains(again, "НДС 10%") {
		t.Fatal("an issued invoice changed its VAT rate when the workspace rate changed")
	}
}
