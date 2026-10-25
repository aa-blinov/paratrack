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
