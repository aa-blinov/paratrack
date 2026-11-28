package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The run CSV is what the accountant takes into the books: one row per person with
// the run's own numbers, decimal hours and the rate and amount in the run's currency.
func TestPayrollRunCSVWritesPersonRows(t *testing.T) {
	var out bytes.Buffer
	lines := []model.PayrollLine{
		{UserID: 5, Label: "Маша", Seconds: 5400, RateCents: 250000, AmountCents: 375000},
	}
	if err := writePayrollRunCSV(&out, "PAY-7", "RUB", lines, csvDialect{}); err != nil {
		t.Fatal(err)
	}
	want := "run,person,hours,seconds,rate,amount,currency\nPAY-7,Маша,1.50,5400,2500.00,3750.00,RUB\n"
	if out.String() != want {
		t.Fatalf("run CSV =\n%s\nwant\n%s", out.String(), want)
	}
}

// The day breakdown names the week a day falls in, so a sheet sums by week without
// the accountant working out which Monday a date belongs to.
func TestPayrollDayWeekStartFollowsTheWeekSetting(t *testing.T) {
	wednesday := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	if got := weekStartOn(false, wednesday).Format("2006-01-02"); got != "2026-11-09" {
		t.Fatalf("Monday week start = %s, want 2026-11-09", got)
	}
	if got := weekStartOn(true, wednesday).Format("2006-01-02"); got != "2026-11-08" {
		t.Fatalf("Sunday week start = %s, want 2026-11-08", got)
	}
	if strings.Count(payrollDayCSVHeader[2], "week_start") != 1 {
		t.Fatalf("day header lost the week column: %v", payrollDayCSVHeader)
	}
}

// The unapproved-weeks page checks the period a manager asks for; a period that does
// not parse, runs backwards or spans more than the bound falls back to the last eight weeks.
func TestUnapprovedWindowTakesTheAskedPeriodOrFallsBack(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC) // Friday; this week starts Mon 2026-11-09
	window := func(query string) (string, string, bool) {
		req := httptest.NewRequest(http.MethodGet, "/payroll?"+query, nil)
		from, to, asked := unapprovedWindow(req, now)
		return from.Format("2006-01-02"), to.Format("2006-01-02"), asked
	}

	if from, to, asked := window("unapproved_from=2026-10-01&unapproved_to=2026-10-31"); from != "2026-10-01" || to != "2026-10-31" || !asked {
		t.Fatalf("asked period = %s..%s asked=%v, want 2026-10-01..2026-10-31 asked", from, to, asked)
	}
	if from, to, asked := window(""); from != "2026-09-14" || to != "2026-11-08" || asked {
		t.Fatalf("default window = %s..%s asked=%v, want the last eight weeks 2026-09-14..2026-11-08", from, to, asked)
	}
	if _, _, asked := window("unapproved_from=2026-11-01&unapproved_to=2026-10-01"); asked {
		t.Fatal("a backwards period must fall back to the default")
	}
	if _, _, asked := window("unapproved_from=2025-01-01&unapproved_to=2026-11-01"); asked {
		t.Fatal("a period longer than the bound must fall back to the default")
	}
	if _, _, asked := window("unapproved_from=oct&unapproved_to=2026-10-31"); asked {
		t.Fatal("an unreadable date must fall back to the default")
	}
}

// The Excel dialect is the file a Russian spreadsheet opens into columns: a byte order
// mark, ';' between fields and a decimal comma in numbers. The default stays RFC 4180.
func TestPayrollRunCSVExcelDialect(t *testing.T) {
	var out bytes.Buffer
	lines := []model.PayrollLine{{UserID: 5, Label: "Маша", Seconds: 5400, RateCents: 250000, AmountCents: 375000}}
	if err := writePayrollRunCSV(&out, "PAY-7", "RUB", lines, csvDialect{excel: true, lang: "en"}); err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBFPay run;Person;Hours;Seconds;Rate;Amount;Currency\nPAY-7;Маша;1,50;5400;2500,00;3750,00;RUB\n"
	if out.String() != want {
		t.Fatalf("Excel run CSV =\n%q\nwant\n%q", out.String(), want)
	}
}

func TestInvoicesExportExcelDialectKeepsAnEmptyCountEmpty(t *testing.T) {
	var out bytes.Buffer
	issued := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	count := 2
	rows := []appmodel.InvoiceExportLine{
		{InvoiceNumber: "INV-1", IssuedAt: issued, Client: "Ромашка", Label: "Сайт", Seconds: 5400, RateCents: 250000, AmountCents: 375000, Currency: "RUB", Status: "sent", UnapprovedWeeks: &count},
		{InvoiceNumber: "INV-0", IssuedAt: issued, Client: "Ромашка", Label: "Старый", Seconds: 3600, RateCents: 250000, AmountCents: 250000, Currency: "RUB", Status: "paid"},
	}
	if err := writeInvoicesExportCSV(&out, rows, func(int64) string { return "ИП Иванов" }, time.UTC, csvDialect{excel: true, lang: "en"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !strings.HasPrefix(lines[0], "\xEF\xBB\xBFInvoice;") {
		t.Fatalf("the Excel export must start with the byte order mark: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], ";sent;2") || !strings.HasSuffix(lines[2], ";paid;") {
		t.Fatalf("unapproved_weeks: the count must read 2 and the old invoice must stay empty: %q / %q", lines[1], lines[2])
	}
	if !strings.Contains(lines[1], ";1,50;2500,00;3750,00;") {
		t.Fatalf("numbers must use a decimal comma in the Excel dialect: %q", lines[1])
	}
}
