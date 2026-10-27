// The calendar event JSON shape (internal/web/crm_views.go, calendarEventView).

export type CalendarEvent = {
  id: number
  title: string
  starts_at: string
  ends_at: string
  location: string
  notes: string
  project_id: number
  client_id: number
  shared: boolean
  /** The author's switch: keep a copy in their connected provider calendar. */
  sync_push: boolean
  /** Pulled from a provider; the sync owns it and it is never pushed back. */
  imported: boolean
  /** Whole days: starts_at is midnight of the first day and ends_at midnight after the last. */
  all_day: boolean
  participant_ids: number[]
  created_by: number
}

// starts_at and ends_at arrive already written in the workspace zone, so the wall
// clock is the text itself. The day arithmetic runs in UTC, which has no offset to
// drift: a browser in another zone must show the same day and hour as the server.

const DAY_MS = 24 * 60 * 60 * 1000
const MINUTE_MS = 60 * 1000

/** The day an event instant falls on in the workspace zone, YYYY-MM-DD. */
export const dayOf = (iso: string) => iso.slice(0, 10)

/** The wall clock an event instant shows, YYYY-MM-DDTHH:MM. */
export const wallClockOf = (iso: string) => iso.slice(0, 16)

/** The HH:MM of an event instant. */
export const clockOf = (iso: string) => iso.slice(11, 16)

/** Midnight UTC of a day, in milliseconds, for formatting its name without a zone. */
export function dayMillis(day: string): number {
  const [year, month, date] = day.split("-").map(Number)
  return Date.UTC(year, month - 1, date)
}

/** The day a number of days away, YYYY-MM-DD. */
export function addDays(day: string, days: number): string {
  return new Date(dayMillis(day) + days * DAY_MS).toISOString().slice(0, 10)
}

/** The first day of the week the day falls in: Monday, or Sunday when the workspace says so. */
export function weekStartOf(day: string, startsSunday = false): string {
  const weekday = new Date(dayMillis(day)).getUTCDay()
  return addDays(day, -(startsSunday ? weekday : (weekday + 6) % 7))
}

/** The first day of the month a day falls in, YYYY-MM-DD. */
export const monthStartOf = (day: string) => `${day.slice(0, 7)}-01`

/** A month, given by its first day, moved by a number of months. */
export function addMonths(monthStart: string, months: number): string {
  const [year, month] = monthStart.split("-").map(Number)
  const index = year * 12 + (month - 1) + months
  return `${Math.floor(index / 12)}-${String((index % 12) + 1).padStart(2, "0")}-01`
}

/** A month grid is six whole weeks, so every month is drawn the same way. */
export const MONTH_GRID_DAYS = 42

/** The first day of a month's grid: the week its first day falls in. */
export function monthGridStart(monthStart: string, startsSunday = false): string {
  return weekStartOf(monthStart, startsSunday)
}

/** The last day an end wall clock names for a whole-day event: a midnight end is the day after it. */
export function lastDayOfWallClock(start: string, end: string): string {
  return end.slice(11, 16) === "00:00" && end > start ? addDays(dayOf(end), -1) : dayOf(end)
}

/** The days an all-day event covers, clipped to the window, both ends inclusive. */
export function daysInWindow(event: CalendarEvent, first: string, last: string): string[] {
  const startDay = dayOf(event.starts_at)
  const from = startDay < first ? first : startDay
  const lastDay = addDays(dayOf(event.ends_at), -1)
  const to = lastDay > last ? last : lastDay
  const days: string[] = []
  for (let day = from; day <= to; day = addDays(day, 1)) days.push(day)
  return days
}

/** A wall clock moved by minutes; it may cross midnight. */
export function addMinutes(wallClock: string, minutes: number): string {
  const [day, clock] = wallClock.split("T")
  const [hours, mins] = clock.split(":").map(Number)
  return new Date(dayMillis(day) + (hours * 60 + mins + minutes) * MINUTE_MS).toISOString().slice(0, 16)
}

/** The first full hour after a wall clock: the default start of a new event. */
export function nextFullHour(wallClock: string): string {
  return addMinutes(`${wallClock.slice(0, 13)}:00`, 60)
}
