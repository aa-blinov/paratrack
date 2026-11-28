package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// Sberbank's public БИК and correspondent account, and a settlement account
// whose control key checks out against them.
const qrSeller = "ООО «Ромашка», ИНН/КПП 7707083893/770701001\nр/с 40702810938000000001 в ПАО Сбербанк\nБИК 044525225, к/с 30101810400000000225"

func TestPaymentQRPayload(t *testing.T) {
	inv := invoiceVM{Number: "INV-2026-007", Status: "sent", Currency: "RUB", TotalCents: 1250050, RemainingCents: 1250050, IssuedLabel: "28.11.2026", SellerDetails: qrSeller}
	got, ok := paymentQR(inv, "Студия")
	want := "ST00012|Name=ООО «Ромашка»|PersonalAcc=40702810938000000001|BankName=ПАО Сбербанк|BIC=044525225|CorrespAcc=30101810400000000225|PayeeINN=7707083893|KPP=770701001|Sum=1250050|Purpose=Оплата по счёту № INV-2026-007 от 28.11.2026. Без НДС"
	if !ok || got != want {
		t.Fatalf("payload:\n got %q\nwant %q", got, want)
	}
	// Half paid: the code asks for what is still owed, not the total again.
	inv.RemainingCents = 500000
	if got, _ := paymentQR(inv, "Студия"); !strings.Contains(got, "|Sum=500000|") {
		t.Fatalf("a half-paid invoice asks for %q", got)
	}
	// Paid: no code at all.
	inv.Status, inv.RemainingCents = "paid", 0
	if _, ok := paymentQR(inv, "Студия"); ok {
		t.Fatal("a paid invoice carries a payment code")
	}
	for name, bad := range map[string]invoiceVM{
		"another currency":  {Number: "X", Currency: "USD", TotalCents: 100, RemainingCents: 100, SellerDetails: qrSeller},
		"nothing owed":      {Number: "X", Currency: "RUB", TotalCents: 0, SellerDetails: qrSeller},
		"no account":        {Number: "X", Currency: "RUB", TotalCents: 100, RemainingCents: 100, SellerDetails: "ООО «Ромашка», ИНН 7707083893"},
		"a wrong БИК digit": {Number: "X", Currency: "RUB", TotalCents: 100, RemainingCents: 100, SellerDetails: "р/с 40702810938000000001 в ПАО Сбербанк\nБИК 044525226"},
	} {
		if _, ok := paymentQR(bad, "Студия"); ok {
			t.Errorf("%s: a payment code was made", name)
		}
	}
}

// The code is drawn on the invoice PDF when the details allow it, and not
// on the act, which asks for no payment.
func TestPaymentQROnTheInvoicePDF(t *testing.T) {
	inv := invoiceVM{Number: "INV-2026-007", Status: "sent", Currency: "RUB", TotalCents: 1250050, RemainingCents: 1250050, Total: "12 500,50 ₽", SellerDetails: qrSeller, ClientName: "Клиент"}
	hasImage := func(act bool) bool {
		pdf, err := renderDocPDF(inv, "Студия", i18n.Lang("ru"), act)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Contains(pdf, []byte("/Subtype /Image"))
	}
	if !hasImage(false) {
		t.Fatal("the invoice PDF has no payment code")
	}
	if hasImage(true) {
		t.Fatal("the act carries a payment code")
	}
	// A long invoice: the code moves to a page of its own rather than run
	// past the bottom margin.
	for i := 0; i < 40; i++ {
		inv.Lines = append(inv.Lines, invoiceLineVM{Label: "Вёрстка", Hours: "1,00", Rate: "1 000,00 ₽", Amount: "1 000,00 ₽"})
	}
	pdf, err := renderDocPDF(inv, "Студия", i18n.Lang("ru"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdf, []byte("/Subtype /Image")) || bytes.Count(pdf, []byte("/Type /Page\n")) < 2 {
		t.Fatalf("a long invoice: image=%v pages=%d", bytes.Contains(pdf, []byte("/Subtype /Image")), bytes.Count(pdf, []byte("/Type /Page\n")))
	}
	inv.Lines = nil
	inv.SellerDetails = "ООО «Ромашка»"
	if hasImage(false) {
		t.Fatal("a payment code was drawn without an account")
	}
}
