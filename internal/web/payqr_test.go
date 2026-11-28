package web

import (
	"bytes"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// Sberbank's public БИК and correspondent account, and a settlement account
// whose control key checks out against them.
const qrSeller = "ООО «Ромашка», ИНН/КПП 7707083893/770701001\nр/с 40702810938000000001 в ПАО Сбербанк\nБИК 044525225, к/с 30101810400000000225"

func TestPaymentQRPayload(t *testing.T) {
	inv := invoiceVM{Number: "INV-2026-007", Currency: "RUB", TotalCents: 1250050, SellerDetails: qrSeller}
	got, ok := paymentQR(inv, "Студия")
	want := "ST00012|Name=ООО «Ромашка»|PersonalAcc=40702810938000000001|BankName=ПАО Сбербанк|BIC=044525225|CorrespAcc=30101810400000000225|PayeeINN=7707083893|KPP=770701001|Sum=1250050|Purpose=Оплата по счёту № INV-2026-007"
	if !ok || got != want {
		t.Fatalf("payload:\n got %q\nwant %q", got, want)
	}
	for name, bad := range map[string]invoiceVM{
		"another currency":  {Number: "X", Currency: "USD", TotalCents: 100, SellerDetails: qrSeller},
		"nothing owed":      {Number: "X", Currency: "RUB", TotalCents: 0, SellerDetails: qrSeller},
		"no account":        {Number: "X", Currency: "RUB", TotalCents: 100, SellerDetails: "ООО «Ромашка», ИНН 7707083893"},
		"a wrong БИК digit": {Number: "X", Currency: "RUB", TotalCents: 100, SellerDetails: "р/с 40702810938000000001 в ПАО Сбербанк\nБИК 044525226"},
	} {
		if _, ok := paymentQR(bad, "Студия"); ok {
			t.Errorf("%s: a payment code was made", name)
		}
	}
}

// The code is drawn on the invoice PDF when the details allow it, and not
// on the act, which asks for no payment.
func TestPaymentQROnTheInvoicePDF(t *testing.T) {
	inv := invoiceVM{Number: "INV-2026-007", Currency: "RUB", TotalCents: 1250050, Total: "12 500,50 ₽", SellerDetails: qrSeller, ClientName: "Клиент"}
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
	inv.SellerDetails = "ООО «Ромашка»"
	if hasImage(false) {
		t.Fatal("a payment code was drawn without an account")
	}
}
