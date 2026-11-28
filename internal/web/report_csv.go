package web

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/money"
)

// csvNeutralizeCell stops a spreadsheet from reading a cell as a formula. A
// leading =, +, -, @ or tab is how Excel and 1С decide a cell computes; an
// apostrophe in front makes the same text a literal. Only cells that can
// carry user-entered names and notes pass through it, never numbers or dates.
func csvNeutralizeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// reportCSV says how a report's file is cut: the scope it was narrowed to, a
// column on every row so the file names its project or client on its own, and
// whether the internal rates and amounts stay in (a file for the client
// leaves them out).
type reportCSV struct {
	scope   string
	noMoney bool
}

// writeReportCSV writes a report as CSV in the dialect. A rate that varies
// inside a row is left empty, as it is on screen.
func writeReportCSV(w io.Writer, rows []reportRow, d csvDialect, cut reportCSV) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	header := []string{"key", "hours", "seconds"}
	if !cut.noMoney {
		header = append(header, "rate", "amount", "currency")
	}
	header = append(header, "share")
	if cut.scope != "" {
		header = append(header, "scope")
	}
	if err := out.Write(d.header(header)); err != nil {
		return err
	}
	for _, row := range rows {
		cells := []string{csvNeutralizeCell(row.Key), d.number(formatHoursDecimal(row.Secs)), strconv.Itoa(row.Secs)}
		if !cut.noMoney {
			rate := ""
			if !row.RateVaries {
				rate = d.number(money.FormatCents(row.RateCents))
			}
			cells = append(cells, rate, d.number(money.FormatCents(row.AmountCents)), row.Currency)
		}
		cells = append(cells, d.number(fmt.Sprintf("%.1f", row.Share)))
		if cut.scope != "" {
			cells = append(cells, csvNeutralizeCell(cut.scope))
		}
		if err := out.Write(cells); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// taskReportCSVHeader is the column order of the task report's CSV (TASK-04). The
// status is the stored code, so a spreadsheet filters on the same words the app uses.
var taskReportCSVHeader = []string{"task", "project", "assignee", "status", "estimate_hours", "tracked_hours", "tracked_seconds", "due"}

// writeTaskReportCSV writes the task report as RFC 4180 CSV, hours with a decimal
// point as in writeReportCSV. An empty estimate stays empty, not 0.00.
func writeTaskReportCSV(w io.Writer, rows []taskReportRow, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(d.header(taskReportCSVHeader)); err != nil {
		return err
	}
	for _, row := range rows {
		estimate := ""
		if row.EstimateMin > 0 {
			estimate = d.number(formatHoursDecimal(row.EstimateMin * 60))
		}
		if err := out.Write([]string{
			csvNeutralizeCell(row.Title), csvNeutralizeCell(row.ProjectName), csvNeutralizeCell(row.AssigneeName), row.Status, estimate,
			d.number(formatHoursDecimal(row.Seconds)), strconv.Itoa(row.Seconds), d.date(row.Due),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// invoiceSessionsCSVHeader is the column order of an invoice's sessions appendix (FIN-06).
var invoiceSessionsCSVHeader = []string{"date", "start", "end", "person", "project", "activity", "hours", "seconds"}

// writeInvoiceSessionsCSV writes the billed sessions, one row each, in the zone the
// viewer reads the time in, in the dialect.
func writeInvoiceSessionsCSV(w io.Writer, rows []appmodel.InvoiceSessionRow, loc *time.Location, hidePeople bool, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(d.header(invoiceSessionsCSVHeader)); err != nil {
		return err
	}
	for _, row := range rows {
		start, end := row.Start.In(loc), row.End.In(loc)
		// "Hide the executors' names" covers the appendix too: the invoice
		// leaving names out while its breakdown listed them hid nothing.
		person := row.Person
		if hidePeople {
			person = ""
		}
		if err := out.Write([]string{
			d.date(start.Format("2006-01-02")), start.Format("15:04"), end.Format("15:04"),
			csvNeutralizeCell(person), csvNeutralizeCell(row.Project), csvNeutralizeCell(row.Activity),
			d.number(formatHoursDecimal(row.Seconds)), strconv.Itoa(row.Seconds),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// invoicesExportCSVHeader is the column order of the accountant's invoice export (FIN-05).
var invoicesExportCSVHeader = []string{"invoice", "date", "client", "executor", "position", "hours", "rate", "amount", "currency", "status", "unapproved_weeks"}

// csvDialect is how an export writes its file. The default is RFC 4180: a
// comma between columns, a decimal point, ISO dates and the column codes as
// headers, for a program to read. The Excel dialect (?format=excel or
// ?excel=1) is for a person in Russian Excel or 1С: a byte order mark, ';'
// between columns, a decimal comma, dates as ДД.ММ.ГГГГ, headers in the
// reader's language, and numbers like an ИНН kept as text.
type csvDialect struct {
	excel bool
	lang  i18n.Lang
}

func dialectOf(r *http.Request) csvDialect {
	q := r.URL.Query()
	return csvDialect{excel: q.Get("format") == "excel" || q.Get("excel") == "1", lang: resolveLang(r)}
}

// excelDialect is the dialect of a file meant for a person, whatever the address says.
func excelDialect(r *http.Request) csvDialect { return csvDialect{excel: true, lang: resolveLang(r)} }

// header names the columns: codes for a program, words for a person.
func (d csvDialect) header(codes []string) []string {
	if !d.excel {
		return codes
	}
	out := make([]string, len(codes))
	for i, code := range codes {
		key := "csv." + code
		if name := i18n.T(d.lang, key); name != key {
			out[i] = name
		} else {
			out[i] = code
		}
	}
	return out
}

// date writes an ISO day as the person reads it in the Excel dialect.
func (d csvDialect) date(iso string) string {
	if !d.excel {
		return iso
	}
	if day, err := time.Parse("2006-01-02", iso); err == nil {
		return day.Format("02.01.2006")
	}
	return iso
}

// digits keeps a code made of digits (ИНН, КПП) as text in Excel, which would
// otherwise drop its leading zeros or print 7,70123E+11. Only digits pass this
// way, so the formula form cannot carry anything else.
func (d csvDialect) digits(s string) string {
	if !d.excel || s == "" {
		return s
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return csvNeutralizeCell(s)
		}
	}
	return `="` + s + `"`
}

// writer starts a CSV file in the dialect. The byte order mark goes before the header.
func (d csvDialect) writer(w io.Writer) (*csv.Writer, error) {
	if d.excel {
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return nil, err
		}
	}
	out := csv.NewWriter(w)
	if d.excel {
		out.Comma = ';'
	}
	return out, nil
}

// number writes a decimal number in the dialect: a decimal comma for Excel, a point otherwise.
func (d csvDialect) number(s string) string {
	if d.excel {
		return strings.ReplaceAll(s, ".", ",")
	}
	return s
}

// writeInvoicesExportCSV writes one row per invoice line. Amounts are decimal in the
// currency of the line; the executor is the workspace's seller name on every row.
func writeInvoicesExportCSV(w io.Writer, rows []appmodel.InvoiceExportLine, executor func(int64) string, loc *time.Location, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(d.header(invoicesExportCSVHeader)); err != nil {
		return err
	}
	for _, row := range rows {
		if err := out.Write([]string{
			row.InvoiceNumber, d.date(exportIssueDay(row, loc)), csvNeutralizeCell(row.Client), csvNeutralizeCell(executor(row.InvoiceID)), csvNeutralizeCell(row.Label),
			d.number(formatHoursDecimal(row.Seconds)), d.number(money.FormatCents(row.RateCents)), d.number(money.FormatCents(row.AmountCents)),
			row.Currency, row.Status, unapprovedWeeksCell(row.UnapprovedWeeks),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// unapprovedWeeksCell is empty for an invoice made before the count was recorded, so a
// spreadsheet cannot read the gap as "nothing unapproved".
func unapprovedWeeksCell(weeks *int) string {
	if weeks == nil {
		return ""
	}
	return strconv.Itoa(*weeks)
}

// formatHoursDecimal writes seconds as hours with two digits after a decimal point,
// rounded the way the invoices round.
func formatHoursDecimal(seconds int) string {
	hundredths := money.HoursHundredths(seconds)
	return fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)
}

// paymentLedgerCSVHeader is the column order of the received-payments ledger.
var paymentLedgerCSVHeader = []string{"paid_on", "invoice", "client", "client_inn", "amount", "currency", "source", "note"}

// writePaymentLedgerCSV writes one row per received payment, amounts decimal
// in the invoice's currency.
func writePaymentLedgerCSV(w io.Writer, rows []appmodel.PaymentLedgerRow, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(d.header(paymentLedgerCSVHeader)); err != nil {
		return err
	}
	for _, row := range rows {
		if err := out.Write([]string{
			d.date(row.PaidOn), row.InvoiceNumber, csvNeutralizeCell(row.Client), d.digits(row.ClientINN),
			d.number(money.FormatCents(row.AmountCents)), row.Currency, d.source(row.Source), csvNeutralizeCell(row.Note),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// source names how a payment came in: the code for a program, a word for a person.
func (d csvDialect) source(code string) string {
	if !d.excel {
		return code
	}
	return paymentSource(d.lang, code)
}

// exportIssueDay is the day the document states: the kept issue day, or the
// creation day of an invoice issued before the day was kept.
func exportIssueDay(row appmodel.InvoiceExportLine, loc *time.Location) string {
	if row.IssuedOn != "" {
		return row.IssuedOn
	}
	return row.IssuedAt.In(loc).Format("2006-01-02")
}
