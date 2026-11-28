package web

import (
	"fmt"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/aa-blinov/paratrack/internal/model"
)

// paymentQR is the payment code a Russian bank app reads (ГОСТ Р 56042-2014,
// "ST00012": the 2 says UTF-8). It is printed only when the invoice is in
// roubles, owes money, and its seller details name an account, a БИК and a
// bank that check out: a code that pays the wrong account is worse than none.
// It asks for what is still owed, so a half-paid invoice does not ask for the
// whole sum again, and a paid one carries no code at all.
func paymentQR(inv invoiceVM, sellerName string) (payload string, ok bool) {
	if strings.ToUpper(inv.Currency) != "RUB" || inv.Status == "paid" || inv.Status == "void" || inv.RemainingCents <= 0 {
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
	// The client's accountant reads the purpose: the invoice's date and how
	// VAT stands, so the payment needs no follow-up question.
	// The purpose is read by a Russian bank and accountant whatever language
	// the screen speaks, so the date is written ДД.ММ.ГГГГ.
	purpose := "Оплата по счёту № " + inv.Number
	if day, err := time.Parse("2006-01-02", inv.IssuedISO); err == nil {
		purpose += " от " + day.Format("02.01.2006")
	} else if inv.IssuedLabel != "" {
		purpose += " от " + inv.IssuedLabel
	}
	if inv.VATIncluded != "" {
		purpose += ". " + inv.VATIncluded
	} else {
		purpose += ". Без НДС"
	}
	parts = append(parts,
		fmt.Sprintf("Sum=%d", inv.RemainingCents),
		"Purpose="+clean(truncateRunes(purpose, 210)),
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
