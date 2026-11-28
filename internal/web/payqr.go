package web

import (
	"fmt"
	"strings"

	"rsc.io/qr"

	"github.com/aa-blinov/paratrack/internal/model"
)

// paymentQR is the payment code a Russian bank app reads (ГОСТ Р 56042-2014,
// "ST00012": the 2 says UTF-8). It is printed only when the invoice is in
// roubles, owes money, and its seller details name an account, a БИК and a
// bank that check out: a code that pays the wrong account is worse than none.
func paymentQR(inv invoiceVM, sellerName string) (payload string, ok bool) {
	if strings.ToUpper(inv.Currency) != "RUB" || inv.TotalCents <= 0 {
		return "", false
	}
	f := model.ParseRequisites(inv.SellerDetails)
	if f.Acc == "" || f.BIK == "" || f.Bank == "" || len(model.RequisiteProblems(inv.SellerDetails)) > 0 {
		return "", false
	}
	name := f.Org
	if name == "" {
		name = sellerName
	}
	// "|" separates the fields, so it never appears inside one.
	clean := func(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "|", " ")) }
	parts := []string{
		"ST00012",
		"Name=" + clean(name),
		"PersonalAcc=" + f.Acc,
		"BankName=" + clean(f.Bank),
		"BIC=" + f.BIK,
	}
	if f.Corr != "" {
		parts = append(parts, "CorrespAcc="+f.Corr)
	}
	if f.INN != "" {
		parts = append(parts, "PayeeINN="+f.INN)
	}
	if f.KPP != "" {
		parts = append(parts, "KPP="+f.KPP)
	}
	parts = append(parts,
		fmt.Sprintf("Sum=%d", inv.TotalCents),
		"Purpose="+clean("Оплата по счёту № "+inv.Number),
	)
	return strings.Join(parts, "|"), true
}

// paymentQRPNG draws the payment code as a PNG, or nil when there is none.
func paymentQRPNG(inv invoiceVM, sellerName string) []byte {
	payload, ok := paymentQR(inv, sellerName)
	if !ok {
		return nil
	}
	code, err := qr.Encode(payload, qr.M)
	if err != nil {
		return nil
	}
	return code.PNG()
}
