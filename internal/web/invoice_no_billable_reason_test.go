package web

import (
	"net/http"
	"net/url"
	"testing"
)

// An invoice for a period with no unbilled time is refused, and the invoices page the
// refusal lands on says why, as an error. The reason used to vanish: its ";" cut the
// flash out of the address, so the page showed nothing at all.
func TestInvoiceForAnEmptyPeriodSaysWhy(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-no-billable@x.test")
	e.chooseSections("studio")

	resp := e.do("POST", "/invoices", url.Values{
		"client": {"Клиент без часов"}, "start": {"2020-01-06"}, "end": {"2020-01-12"},
	}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create the invoice: status %d, want a redirect", resp.StatusCode)
	}
	page := reactData[invoicesPage](t, readBody(t, e.do("GET", resp.Header.Get("Location"), nil, nil)))
	const want = "За этот период нечего выставлять. В счёт попадают завершённые сессии оплачиваемых проектов со ставкой, которых ещё нет в других счетах; идущий таймер сначала остановите."
	if page.Flash != want || page.FlashOK {
		t.Fatalf("the invoices page shows flash %q (ok %v), want the refusal %q as an error", page.Flash, page.FlashOK, want)
	}
}
