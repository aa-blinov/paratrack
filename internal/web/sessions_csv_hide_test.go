package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// With executors' names hidden on invoices, the appendix leaves the person
// column empty too; otherwise it names them.
func TestInvoiceSessionsCSVHidesPeopleWhenAsked(t *testing.T) {
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	rows := []appmodel.InvoiceSessionRow{{Start: start, End: start.Add(time.Hour), Person: "Анна Петрова", Project: "Сайт", Activity: "Вёрстка", Seconds: 3600}}
	var shown, hidden bytes.Buffer
	if err := writeInvoiceSessionsCSV(&shown, rows, time.UTC, false, csvDialect{}); err != nil {
		t.Fatal(err)
	}
	if err := writeInvoiceSessionsCSV(&hidden, rows, time.UTC, true, csvDialect{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "Анна Петрова") {
		t.Fatalf("names shown: %s", shown.String())
	}
	if strings.Contains(hidden.String(), "Анна") || !strings.Contains(hidden.String(), "2026-11-02,10:00,11:00,,Сайт,Вёрстка,1.00,3600") {
		t.Fatalf("names hidden: %s", hidden.String())
	}
}

// The Excel dialect speaks to a person: Russian headers, ';', a decimal
// comma, ДД.ММ.ГГГГ, and an ИНН kept as text; a report for the client leaves
// the rates out and names its scope on every row.
func TestExcelDialectIsReadableInRussianExcel(t *testing.T) {
	d := csvDialect{excel: true, lang: "ru"}
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	var sessions bytes.Buffer
	if err := writeInvoiceSessionsCSV(&sessions, []appmodel.InvoiceSessionRow{{Start: start, End: start.Add(90 * time.Minute), Person: "Анна", Project: "Сайт", Activity: "Вёрстка", Seconds: 5400}}, time.UTC, false, d); err != nil {
		t.Fatal(err)
	}
	got := sessions.String()
	if !strings.HasPrefix(got, "\ufeffДата;Начало;Конец;Исполнитель;Проект;Работа;Часы;Секунды") || !strings.Contains(got, "02.11.2026;10:00;11:30;Анна;Сайт;Вёрстка;1,50;5400") {
		t.Fatalf("sessions for Excel: %q", got)
	}
	var ledger bytes.Buffer
	if err := writePaymentLedgerCSV(&ledger, []appmodel.PaymentLedgerRow{{PaidOn: "2026-11-03", InvoiceNumber: "INV-1", Client: "Ромашка", ClientINN: "012345678901", AmountCents: 150000, Currency: "RUB", Source: "manual"}}, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ledger.String(), `03.11.2026;INV-1;Ромашка;"=""012345678901""";1500,00`) {
		t.Fatalf("ledger for Excel: %q", ledger.String())
	}
	var report bytes.Buffer
	if err := writeReportCSV(&report, []reportRow{{Key: "Сайт", Secs: 3600, RateCents: 500000, AmountCents: 500000, Currency: "RUB", Share: 100}}, d, reportCSV{scope: "Клиент: Ромашка", noMoney: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report.String(), "5000") || !strings.Contains(report.String(), "Сайт;1,00;3600;100,0;Клиент: Ромашка") {
		t.Fatalf("client report: %q", report.String())
	}
}

// The 1С dialect is Excel's but keeps an ИНН as bare digits.
func TestOneCDialectKeepsCodesBare(t *testing.T) {
	var ledger bytes.Buffer
	if err := writePaymentLedgerCSV(&ledger, []appmodel.PaymentLedgerRow{{PaidOn: "2026-11-03", InvoiceNumber: "INV-1", Client: "Ромашка", ClientINN: "012345678901", AmountCents: 150000, Currency: "RUB", Source: "manual"}}, csvDialect{excel: true, oneC: true, lang: "ru"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ledger.String(), "03.11.2026;INV-1;Ромашка;012345678901;1500,00") {
		t.Fatalf("ledger for 1С: %q", ledger.String())
	}
}
