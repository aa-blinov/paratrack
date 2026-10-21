package web

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

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

// formatHoursDecimal writes seconds as hours with two digits after a decimal point,
// rounded the way the invoices round.
func formatHoursDecimal(seconds int) string {
	hundredths := money.HoursHundredths(seconds)
	return fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)
}
