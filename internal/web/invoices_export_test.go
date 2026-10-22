package web

import (
	"net/http"
	"strings"
	"testing"
)

// The accountant's download answers with the CSV header on an empty workspace, and a
// period that ends before it starts is refused (FIN-05).
func TestAccountantInvoiceExportAnswersAsCSV(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-export@x.test")

	response := e.do("GET", "/invoices/export.csv?export_from=2026-10-01&export_to=2026-10-31&export_client=Ромашка", nil, nil)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("the export answered %d as %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(body, "invoice,date,client,executor,position,hours,rate,amount,currency,status\n") {
		t.Fatalf("the export header is wrong: %q", body)
	}

	inverted := e.do("GET", "/invoices/export.csv?export_from=2026-10-31&export_to=2026-10-01", nil, nil)
	readBody(t, inverted)
	if inverted.StatusCode != http.StatusBadRequest {
		t.Fatalf("an inverted export period answered %d, want 400", inverted.StatusCode)
	}
}
