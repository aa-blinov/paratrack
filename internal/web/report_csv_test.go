package web

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"testing"
)

// The billable report's CSV is read by a spreadsheet and an accountant: every
// record has the header's fields, hours carry a decimal point, money is in the
// row's currency, and a key with a comma or a quote survives.
func TestBillableReportCSVReadsBackAsAFlatTable(t *testing.T) {
	rows := []reportRow{
		{Key: "Сайт, каталог", Secs: 73800, RateCents: 500000, AmountCents: 10250000, Currency: "RUB", Share: 62.5},
		{Key: `Кавычка "А"`, Secs: 3600, RateVaries: true, AmountCents: 6150000, Currency: "RUB", Share: 37.5},
	}
	var buf bytes.Buffer
	if err := writeReportCSV(&buf, rows); err != nil {
		t.Fatalf("write the report: %v", err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("read the report back as CSV: %v", err)
	}
	if !reflect.DeepEqual(records[0], []string{"key", "hours", "seconds", "rate", "amount", "currency", "share"}) {
		t.Fatalf("header = %q", records[0])
	}
	want := [][]string{
		{"Сайт, каталог", "20.50", "73800", "5000.00", "102500.00", "RUB", "62.5"},
		{`Кавычка "А"`, "1.00", "3600", "", "61500.00", "RUB", "37.5"},
	}
	if !reflect.DeepEqual(records[1:], want) {
		t.Fatalf("records = %q, want %q", records[1:], want)
	}
}

// Hours round the way the invoices round, with two digits after a decimal point
// in any language.
func TestFormatHoursDecimalUsesAPointAndTwoDigits(t *testing.T) {
	cases := map[int]string{0: "0.00", 1800: "0.50", 5400: "1.50", 73800: "20.50", 7201: "2.00"}
	for seconds, want := range cases {
		if got := formatHoursDecimal(seconds); got != want {
			t.Fatalf("formatHoursDecimal(%d) = %q, want %q", seconds, got, want)
		}
	}
}
