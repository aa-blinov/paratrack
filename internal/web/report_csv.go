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
	"github.com/aa-blinov/paratrack/internal/money"
)

// reportCSVHeader is the column order of a report's CSV. Amounts and rates are in
// the currency of their row, with two digits and no minor units in the column name.
var reportCSVHeader = []string{"key", "hours", "seconds", "rate", "amount", "currency", "share"}

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

// writeReportCSV writes a report as RFC 4180 CSV. Hours use a decimal point in any
// language, so a spreadsheet and an accountant read the same number. A rate that
// varies inside a row is left empty, as it is on screen.
func writeReportCSV(w io.Writer, rows []reportRow) error {
	out := csv.NewWriter(w)
	if err := out.Write(reportCSVHeader); err != nil {
		return err
	}
	for _, row := range rows {
		rate := ""
		if !row.RateVaries {
			rate = money.FormatCents(row.RateCents)
		}
		if err := out.Write([]string{
			csvNeutralizeCell(row.Key), formatHoursDecimal(row.Secs), strconv.Itoa(row.Secs), rate,
			money.FormatCents(row.AmountCents), row.Currency, fmt.Sprintf("%.1f", row.Share),
		}); err != nil {
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
func writeTaskReportCSV(w io.Writer, rows []taskReportRow) error {
	out := csv.NewWriter(w)
	if err := out.Write(taskReportCSVHeader); err != nil {
		return err
	}
	for _, row := range rows {
		estimate := ""
		if row.EstimateMin > 0 {
			estimate = formatHoursDecimal(row.EstimateMin * 60)
		}
		if err := out.Write([]string{
			csvNeutralizeCell(row.Title), csvNeutralizeCell(row.ProjectName), csvNeutralizeCell(row.AssigneeName), row.Status, estimate,
			formatHoursDecimal(row.Seconds), strconv.Itoa(row.Seconds), row.Due,
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
// viewer reads the time in. Hours use a decimal point, as in the other exports.
func writeInvoiceSessionsCSV(w io.Writer, rows []appmodel.InvoiceSessionRow, loc *time.Location, hidePeople bool) error {
	out := csv.NewWriter(w)
	if err := out.Write(invoiceSessionsCSVHeader); err != nil {
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
			start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"),
			csvNeutralizeCell(person), csvNeutralizeCell(row.Project), csvNeutralizeCell(row.Activity),
			formatHoursDecimal(row.Seconds), strconv.Itoa(row.Seconds),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// invoicesExportCSVHeader is the column order of the accountant's invoice export (FIN-05).
var invoicesExportCSVHeader = []string{"invoice", "date", "client", "executor", "position", "hours", "rate", "amount", "currency", "status", "unapproved_weeks"}

// csvDialect is how an accountant's export writes its file. The default is RFC 4180:
// a comma between columns and a decimal point in numbers. The Excel dialect
// (?format=excel) starts with a byte order mark, puts ';' between columns and writes a
// decimal comma, which Russian Excel and 1С read as one column per field.
type csvDialect struct{ excel bool }

func dialectOf(r *http.Request) csvDialect {
	return csvDialect{excel: r.URL.Query().Get("format") == "excel"}
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
func writeInvoicesExportCSV(w io.Writer, rows []appmodel.InvoiceExportLine, executor string, loc *time.Location, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(invoicesExportCSVHeader); err != nil {
		return err
	}
	for _, row := range rows {
		if err := out.Write([]string{
			row.InvoiceNumber, row.IssuedAt.In(loc).Format("2006-01-02"), csvNeutralizeCell(row.Client), csvNeutralizeCell(executor), csvNeutralizeCell(row.Label),
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
