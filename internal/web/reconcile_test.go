package web

import (
	"testing"
)

// The reconciliation card answers a window the manager chose, and says why a window that
// ends before it starts is refused (FIN-04).
func TestReconciliationCardAnswersAWindow(t *testing.T) {
	e := newAPIEnv(t)
	e.register("reconcile@x.test")

	inverted := reactData[invoicesPage](t, readBody(t, e.do("GET", "/invoices?reconcile_from=2026-10-21&reconcile_to=2026-10-20", nil, nil)))
	if inverted.Reconcile == nil || inverted.Reconcile.Note == "" {
		t.Fatalf("an inverted window shows no reason: %+v", inverted.Reconcile)
	}

	page := reactData[invoicesPage](t, readBody(t, e.do("GET", "/invoices?reconcile_from=2026-10-01&reconcile_to=2026-10-31", nil, nil)))
	if page.Reconcile == nil || page.Reconcile.Note != "" || page.Reconcile.Rows == nil {
		t.Fatalf("a valid window answered %+v", page.Reconcile)
	}
	if page.Reconcile.From != "2026-10-01" || page.Reconcile.To != "2026-10-31" {
		t.Fatalf("the window reads back as %q to %q", page.Reconcile.From, page.Reconcile.To)
	}
}
