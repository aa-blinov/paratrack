package web

import (
	"net/url"
	"testing"
)

// The invoices screen says when the issuer's details are blank, because an invoice
// issued then carries only the name, and stops saying it once the details are saved (ADM-17).
func TestInvoicesWarnWhileRequisitesAreBlank(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-requisites@x.test")

	if !reactData[invoicesPage](t, readBody(t, e.do("GET", "/invoices", nil, nil))).RequisitesEmpty {
		t.Fatal("a workspace with no requisites does not warn on the invoices screen")
	}

	saved := e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП Иванова А. С., ИНН 770000000000"}}, nil)
	readBody(t, saved)
	if reactData[invoicesPage](t, readBody(t, e.do("GET", "/invoices", nil, nil))).RequisitesEmpty {
		t.Fatal("the warning stays after the requisites are saved")
	}
}
