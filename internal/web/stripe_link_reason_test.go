package web

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// A refused key and a missing key name the setting to fix; an outage and any other
// failure keep the general sentence, because nothing in the settings would help.
func TestStripeLinkReasonNamesOnlyWhatTheOwnerCanFix(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   string
		wantOK bool
	}{
		{"key refused", fmt.Errorf("%w: stripe returned status 401", appmodel.ErrStripeKeyRejected), i18n.T(i18n.Ru, "stripe.keyRejected"), true},
		{"key not set", appmodel.ErrStripeUnavailable, i18n.T(i18n.Ru, "stripe.notConfigured"), true},
		{"outage", errors.New("create Stripe checkout: stripe returned status 503"), "", false},
		{"network", errors.New("create Stripe checkout session: dial tcp: no route to host"), "", false},
	}
	for _, tc := range cases {
		got, ok := stripeLinkReason(i18n.Ru, tc.err)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("%s: reason %q (ok %v), want %q (ok %v)", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
	if !strings.Contains(i18n.T(i18n.Ru, "stripe.keyRejected"), "«Онлайн-оплата»") {
		t.Fatal("the key reason does not say where to change the key")
	}
	if strings.Contains(i18n.T(i18n.Ru, "stripe.notConfigured"), "process configuration") {
		t.Fatal("the not-set reason still carries the English text")
	}
}

// A payment link for an invoice with no Stripe key set says so, in Russian, on the
// invoice page as an error.
func TestPaymentLinkWithoutKeySaysWhyOnTheInvoice(t *testing.T) {
	e := newAPIEnv(t)
	e.register("stripe-no-key@x.test")
	e.chooseSections("studio")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Оплата"}, "rate": {"2500"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}, "project_id": {"1"}}, map[string]string{"HX-Request": "true"}))
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	created := e.do("POST", "/invoices", url.Values{"client": {"ООО «Оплата»"}, "start": {day}, "end": {day}}, nil)
	loc := created.Header.Get("Location")
	created.Body.Close()
	if !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("create invoice: %d %s", created.StatusCode, loc)
	}
	pay := e.do("POST", loc+"/pay", url.Values{"mode": {"stripe"}}, nil)
	pay.Body.Close()
	if pay.StatusCode != 303 {
		t.Fatalf("payment link: status %d, want a redirect", pay.StatusCode)
	}
	page := reactData[invoiceDetailPage](t, readBody(t, e.do("GET", pay.Header.Get("Location"), nil, nil)))
	want := i18n.T(i18n.Ru, "stripe.notConfigured")
	if page.Flash != want || page.FlashOK {
		t.Fatalf("invoice page shows flash %q (ok %v), want the not-set reason %q as an error", page.Flash, page.FlashOK, want)
	}
}
