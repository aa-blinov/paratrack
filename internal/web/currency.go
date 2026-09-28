package web

import (
	"sort"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// currencies the UI offers, in menu order. Amounts are stored in minor
// units (cents / kopecks) of the document's currency; nothing converts.
var currencies = []struct{ Code, Symbol string }{
	{"RUB", "₽"}, {"USD", "$"}, {"EUR", "€"}, {"KZT", "₸"}, {"BYN", "Br"},
	{"UAH", "₴"}, {"GBP", "£"}, {"GEL", "₾"}, {"AMD", "AMD"}, {"UZS", "сўм"},
	{"TRY", "₺"}, {"AED", "AED"}, {"CNY", "¥"},
}

type currencyOption struct{ Code, Label string }

// currencyOptions is the <select> menu: "RUB (₽)".
func currencyOptions() []currencyOption {
	out := make([]currencyOption, 0, len(currencies))
	for _, c := range currencies {
		label := c.Code
		if c.Symbol != c.Code {
			label += " (" + c.Symbol + ")"
		}
		out = append(out, currencyOption{c.Code, label})
	}
	return out
}

func currencySymbol(code string) string {
	for _, c := range currencies {
		if c.Code == code {
			return c.Symbol
		}
	}
	return code
}

func validCurrency(code string) bool {
	for _, c := range currencies {
		if c.Code == code {
			return true
		}
	}
	return false
}

// moneyL is an amount as a document shows it: "1 234,56 ₽" in Russian,
// "$1,234.56" / "1,234.56 ₽" in English (only $ € £ go in front).
func moneyL(lang i18n.Lang, cents int, cur string) string {
	n, sym := formatMoneyL(lang, cents), currencySymbol(cur)
	if lang != i18n.Ru && (sym == "$" || sym == "€" || sym == "£") {
		if strings.HasPrefix(n, "-") {
			return "-" + sym + n[1:]
		}
		return sym + n
	}
	return n + " " + sym
}

// moneyByCurrency joins per-currency sums: they are never added together.
func moneyByCurrency(lang i18n.Lang, sums map[string]int) string {
	codes := make([]string, 0, len(sums))
	for c := range sums {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, c := range codes {
		parts = append(parts, moneyL(lang, sums[c], c))
	}
	if len(parts) == 0 {
		return moneyL(lang, 0, "RUB")
	}
	return strings.Join(parts, ", ")
}

// formatMoneyInput is an editable amount: "2500,50" in Russian (no
// thousands gap, so it round-trips through formCents), "2500.50" otherwise.
func formatMoneyInput(lang i18n.Lang, cents int) string {
	if lang == i18n.Ru {
		return strings.Replace(formatMoney(cents), ".", ",", 1)
	}
	return formatMoney(cents)
}
