package web

import (
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// A draft whose tracked time differs from its lines says both hours; equal time,
// and any status other than draft, say nothing.
func TestInvoiceDriftMessageNamesBothHoursOnlyForADraft(t *testing.T) {
	if got, want := invoiceDriftMessage(i18n.Lang("ru"), "draft", 27000, 19800), "Табель изменился: в счёте 7,50 ч, учтено 5,50 ч."; got != want {
		t.Fatalf("drift message = %q, want %q", got, want)
	}
	if got := invoiceDriftMessage(i18n.Lang("ru"), "draft", 3600, 3600); got != "" {
		t.Fatalf("equal time shows a drift message %q", got)
	}
	for _, status := range []string{"sent", "paid"} {
		if got := invoiceDriftMessage(i18n.Lang("ru"), status, 27000, 19800); got != "" {
			t.Fatalf("a %s invoice shows a drift message %q", status, got)
		}
	}
}
