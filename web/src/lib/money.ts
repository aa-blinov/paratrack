// Amounts travel as integer cents. A person types units, so the conversion is
// strict: anything that is not a plain amount reads as null and is never sent.

/** "1500,5" and "1500.50" both mean cents; "" means no amount, which is zero. */
export function parseMoneyCents(text: string): number | null {
  const raw = text.trim().replace(/\s/g, "").replace(",", ".")
  if (raw === "") return 0
  if (!/^\d+(\.\d{1,2})?$/.test(raw)) return null
  return Math.round(Number(raw) * 100)
}

/** The editable form of cents, empty for zero so a new field stays empty. */
export function centsInput(cents: number): string {
  return cents ? String(cents / 100) : ""
}

/** Cents in the workspace currency, spelled in the language of the screen. */
export function formatMoney(cents: number, currency: string, lang: string): string {
  const locale = lang === "en" ? "en-US" : "ru-RU"
  const value = cents / 100
  if (!/^[A-Z]{3}$/.test(currency)) return new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(value)
  return new Intl.NumberFormat(locale, { style: "currency", currency, maximumFractionDigits: 2 }).format(value)
}
