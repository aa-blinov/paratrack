/**
 * Dates in the language of the interface, not of the browser.
 *
 * A native `<input type="date">` draws itself in the *browser's* locale and no
 * attribute moves it: on a machine set to en-US a Russian interface offered
 * «10/13/2026». So the app renders the text itself and keeps a hidden input
 * carrying the same ISO value the server has always received — the forms, the
 * handlers and the stored dates are untouched.
 *
 * Everything is a plain `YYYY-MM-DD` / `YYYY-MM-DDTHH:MM` string in, the same
 * string out. Nothing here knows about time zones: these are wall-clock
 * moments, which is what the whole application stores.
 */

const pad = (value: number) => String(value).padStart(2, "0")

/** ru wants 13.10.2026, en wants 10/13/2026. */
export function formatDate(iso: string, lang: string): string {
  const parts = parseISODate(iso)
  if (!parts) return ""
  const { y, m, d } = parts
  return lang === "en" ? `${pad(m)}/${pad(d)}/${y}` : `${pad(d)}.${pad(m)}.${y}`
}

export function formatDateTime(iso: string, lang: string): string {
  const parts = parseISODateTime(iso)
  if (!parts) return ""
  return `${formatParts(parts, lang)} ${pad(parts.hh)}:${pad(parts.mm)}`
}

function formatParts({ y, m, d }: DateParts, lang: string): string {
  return lang === "en" ? `${pad(m)}/${pad(d)}/${y}` : `${pad(d)}.${pad(m)}.${y}`
}

type DateParts = { y: number; m: number; d: number }
type DateTimeParts = DateParts & { hh: number; mm: number }

export function parseISODate(iso: string): DateParts | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso.trim())
  if (!match) return null
  const [, y, m, d] = match.map(Number) as unknown as number[]
  return validDate(y, m, d) ? { y, m, d } : null
}

export function parseISODateTime(iso: string): DateTimeParts | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/.exec(iso.trim())
  if (!match) return null
  const [, y, m, d, hh, mm] = match.map(Number) as unknown as number[]
  if (!validDate(y, m, d) || hh > 23 || mm > 59) return null
  return { y, m, d, hh, mm }
}

// The date must survive the round trip: 31 February would otherwise roll over
// into March and be silently saved.
function validDate(y: number, m: number, d: number): boolean {
  if (m < 1 || m > 12 || d < 1 || y < 1000) return false
  return d <= new Date(Date.UTC(y, m, 0)).getUTCDate()
}

/**
 * Read what a person typed.
 *
 * The language's own order first («13.10.2026» in ru, «10/13/2026» in en),
 * then ISO either way, because a value pasted from a URL or a spreadsheet is
 * ISO and should just work. Dots always mean day-first and slashes always mean
 * month-first: that is the convention each language actually uses, and
 * guessing would swap a day and a month without telling anyone.
 */
export function parseDateInput(text: string, lang: string): string | null {
  const raw = text.trim()
  if (!raw) return ""

  const iso = parseISODate(raw)
  if (iso) return toISO(iso)

  const groups = /^(\d{1,4})\s*[./-]\s*(\d{1,4})\s*[./-]\s*(\d{1,4})$/.exec(raw)
  if (!groups) return null
  const a = Number(groups[1]); const b = Number(groups[2]); const c = Number(groups[3])

  // Year in front is unambiguous and is what an ISO-ish paste looks like.
  if (groups[1].length === 4) return validDate(a, b, c) ? toISO({ y: a, m: b, d: c }) : null

  // Year last, possibly written with two digits.
  let y = c
  if (groups[3].length === 2) y += c < 70 ? 2000 : 1900
  const dayFirst = lang !== "en"
  const d = dayFirst ? a : b
  const month = dayFirst ? b : a
  return validDate(y, month, d) ? toISO({ y, m: month, d }) : null
}

export function parseDateTimeInput(text: string, lang: string): string | null {
  const raw = text.trim()
  if (!raw) return ""

  // An ISO moment pasted from a URL or an export is already the answer.
  const stamped = parseISODateTime(raw)
  if (stamped) return `${toISO(stamped)}T${pad(stamped.hh)}:${pad(stamped.mm)}`

  const [datePart, timePart, extra] = raw.split(/\s+/)
  if (extra) return null

  const date = parseDateInput(datePart, lang)
  if (date === null) return null

  if (!timePart) {
    // A date with no time keeps the time it already had.
    const existing = parseISODateTime(date)
    return `${date}T${pad(existing ? existing.hh : 0)}:${pad(existing ? existing.mm : 0)}`
  }
  const t = /^(\d{1,2})[:.](\d{2})$/.exec(timePart)
  if (!t) return null
  const hh = Number(t[1]); const mm = Number(t[2])
  if (hh > 23 || mm > 59) return null
  return `${date}T${pad(hh)}:${pad(mm)}`
}

function toISO({ y, m, d }: DateParts): string {
  return `${y}-${pad(m)}-${pad(d)}`
}

/** First weekday: Monday reads as the week start in Russian, Sunday in English. */
export function firstWeekday(lang: string): number {
  return lang === "en" ? 0 : 1
}

export function monthLabel(m: number, lang: string): string {
  const names = lang === "en"
    ? ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"]
    : ["январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"]
  return names[m]
}

/** The six-letter weekday headers, from the chosen first weekday. */
export function weekdayLabels(lang: string): string[] {
  const sundayFirst = lang === "en"
    ? ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"]
    : ["вс", "пн", "вт", "ср", "чт", "пт", "сб"]
  const start = firstWeekday(lang)
  return Array.from({ length: 7 }, (_, i) => sundayFirst[(i + start) % 7])
}