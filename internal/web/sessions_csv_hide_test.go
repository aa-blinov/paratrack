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
	if err := writeInvoiceSessionsCSV(&shown, rows, time.UTC, false); err != nil {
		t.Fatal(err)
	}
	if err := writeInvoiceSessionsCSV(&hidden, rows, time.UTC, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "Анна Петрова") {
		t.Fatalf("names shown: %s", shown.String())
	}
	if strings.Contains(hidden.String(), "Анна") || !strings.Contains(hidden.String(), "2026-11-02,10:00,11:00,,Сайт,Вёрстка,1.00,3600") {
		t.Fatalf("names hidden: %s", hidden.String())
	}
}
