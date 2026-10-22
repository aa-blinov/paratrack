package web

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/money"
)

// reportCSVHeader is the column order of a report's CSV. Amounts and rates are in
// the currency of their row, with two digits and no minor units in the column name.
var reportCSVHeader = []string{"key", "hours", "seconds", "rate", "amount", "currency", "share"}

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
			row.Key, formatHoursDecimal(row.Secs), strconv.Itoa(row.Secs), rate,
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
			row.Title, row.ProjectName, row.AssigneeName, row.Status, estimate,
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
func writeInvoiceSessionsCSV(w io.Writer, rows []appmodel.InvoiceSessionRow, loc *time.Location) error {
	out := csv.NewWriter(w)
	if err := out.Write(invoiceSessionsCSVHeader); err != nil {
		return err
	}
	for _, row := range rows {
		start, end := row.Start.In(loc), row.End.In(loc)
		if err := out.Write([]string{
			start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"),
			row.Person, row.Project, row.Activity, formatHoursDecimal(row.Seconds), strconv.Itoa(row.Seconds),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// invoicesExportCSVHeader is the column order of the accountant's invoice export (FIN-05).
var invoicesExportCSVHeader = []string{"invoice", "date", "client", "executor", "position", "hours", "rate", "amount", "currency", "status"}

// writeInvoicesExportCSV writes one row per invoice line. Amounts are decimal in the
// currency of the line; the executor is the workspace's seller name on every row.
func writeInvoicesExportCSV(w io.Writer, rows []appmodel.InvoiceExportLine, executor string, loc *time.Location) error {
	out := csv.NewWriter(w)
	if err := out.Write(invoicesExportCSVHeader); err != nil {
		return err
	}
	for _, row := range rows {
		if err := out.Write([]string{
			row.InvoiceNumber, row.IssuedAt.In(loc).Format("2006-01-02"), row.Client, executor, row.Label,
			formatHoursDecimal(row.Seconds), money.FormatCents(row.RateCents), money.FormatCents(row.AmountCents),
			row.Currency, row.Status,
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// formatHoursDecimal writes seconds as hours with two digits after a decimal point,
// rounded the way the invoices round.
func formatHoursDecimal(seconds int) string {
	hundredths := money.HoursHundredths(seconds)
	return fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)
}
