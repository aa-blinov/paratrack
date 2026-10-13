import { describe, expect, it } from "vitest"
import {
  firstWeekday, formatDate, formatDateTime, monthLabel, parseDateInput,
  parseDateTimeInput, parseISODate, weekdayLabels,
} from "./date-format"

// Date parsing is the kind of code that fails quietly: a swapped month lands in
// an invoice and nobody notices for a week. The cases below are the ones that
// actually bite, so each one is pinned.

describe("dates in the language of the interface", () => {
  it("writes the day first in Russian and the month first in English", () => {
    expect(formatDate("2026-10-13", "ru")).toBe("13.10.2026")
    expect(formatDate("2026-10-13", "en")).toBe("10/13/2026")
    expect(formatDateTime("2026-10-13T06:41", "ru")).toBe("13.10.2026 06:41")
    expect(formatDateTime("2026-10-13T06:41", "en")).toBe("10/13/2026 06:41")
  })

  it("has nothing to show for an empty or unreadable value", () => {
    expect(formatDate("", "ru")).toBe("")
    expect(formatDate("не дата", "ru")).toBe("")
  })

  it("reads the order the reader's language uses", () => {
    expect(parseDateInput("13.10.2026", "ru")).toBe("2026-10-13")
    expect(parseDateInput("10/13/2026", "en")).toBe("2026-10-13")
    expect(parseDateInput("3.4.2026", "ru")).toBe("2026-04-03")
    // The same numbers mean different days in the two languages, and guessing
    // would swap them without a word.
    expect(parseDateInput("3/4/2026", "en")).toBe("2026-03-04")
    expect(parseDateInput("3/4/2026", "ru")).toBe("2026-04-03")
  })

  it("takes an ISO value in either language, because pastes are ISO", () => {
    expect(parseDateInput("2026-10-13", "ru")).toBe("2026-10-13")
    expect(parseDateInput("2026-10-13", "en")).toBe("2026-10-13")
    expect(parseDateInput("2026/10/13", "ru")).toBe("2026-10-13")
    expect(parseDateInput("2026.10.13", "en")).toBe("2026-10-13")
  })

  it("reads a two-digit year into the century it belongs to", () => {
    expect(parseDateInput("13.10.26", "ru")).toBe("2026-10-13")
    expect(parseDateInput("13.10.90", "ru")).toBe("1990-10-13")
  })

  it("refuses a date that does not exist instead of rolling it over", () => {
    expect(parseDateInput("31.02.2026", "ru")).toBeNull()
    expect(parseDateInput("31.04.2026", "ru")).toBeNull()
    expect(parseDateInput("02/31/2026", "en")).toBeNull()
    expect(parseDateInput("13.13.2026", "ru")).toBeNull()
    expect(parseISODate("2026-02-31")).toBeNull()
    expect(parseDateInput("29.02.2024", "ru")).toBe("2024-02-29")
    expect(parseDateInput("29.02.2026", "ru")).toBeNull()
  })

  it("treats an emptied field as no date at all", () => {
    expect(parseDateInput("", "ru")).toBe("")
    expect(parseDateInput("   ", "en")).toBe("")
  })

  it("keeps a time through a round trip and refuses an impossible one", () => {
    expect(parseDateTimeInput("13.10.2026 06:41", "ru")).toBe("2026-10-13T06:41")
    expect(parseDateTimeInput("10/13/2026 6:41", "en")).toBe("2026-10-13T06:41")
    expect(parseDateTimeInput("13.10.2026 06.41", "ru")).toBe("2026-10-13T06:41")
    expect(parseDateTimeInput("13.10.2026 23:59", "ru")).toBe("2026-10-13T23:59")
    expect(parseDateTimeInput("13.10.2026", "ru")).toBe("2026-10-13T00:00")
    expect(parseDateTimeInput("2026-10-13T08:05", "ru")).toBe("2026-10-13T08:05")
    expect(parseDateTimeInput("2026-10-13T08:05", "en")).toBe("2026-10-13T08:05")
    expect(parseDateTimeInput("2026-10-13T08:05Z", "ru")).toBe("2026-10-13T08:05")
    expect(parseDateTimeInput("13.10.2026 24:00", "ru")).toBeNull()
    expect(parseDateTimeInput("13.10.2026 12:60", "ru")).toBeNull()
    expect(parseDateTimeInput("13.10.2026 06:41 утром", "ru")).toBeNull()
    expect(parseDateTimeInput("", "ru")).toBe("")
  })

  it("starts the week where the language starts it", () => {
    expect(firstWeekday("ru")).toBe(1)
    expect(firstWeekday("en")).toBe(0)
    expect(weekdayLabels("ru")[0]).toBe("пн")
    expect(weekdayLabels("en")[0]).toBe("Su")
    expect(weekdayLabels("ru")).toHaveLength(7)
  })

  it("names the month in the interface's language", () => {
    expect(monthLabel(9, "ru")).toBe("октябрь")
    expect(monthLabel(9, "en")).toBe("October")
  })
})