import { describe, expect, it } from "vitest"
import { addDays, addMinutes, addMonths, clockOf, daysInWindow, lastDayOfWallClock, monthGridStart, monthStartOf, nextFullHour, weekStartOf, type CalendarEvent } from "./model"

// The calendar reads days and times off the workspace wall clock, never the browser's
// zone, so these must hold under any TZ (run them with TZ set to a far zone too).

describe("calendar days and times", () => {
  it("starts the week on Monday and moves whole days across month and year ends", () => {
    expect(weekStartOf("2026-10-28")).toBe("2026-10-26")
    expect(weekStartOf("2026-10-25")).toBe("2026-10-19")
    expect(weekStartOf("2026-10-26")).toBe("2026-10-26")
    expect(weekStartOf("2026-10-28", true)).toBe("2026-10-25")
    expect(weekStartOf("2026-10-25", true)).toBe("2026-10-25")
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01")
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28")
  })

  it("moves a wall clock across midnight and reads the clock of an instant", () => {
    expect(addMinutes("2026-10-24T23:30", 60)).toBe("2026-10-25T00:30")
    expect(nextFullHour("2026-10-24T09:23")).toBe("2026-10-24T10:00")
    expect(clockOf("2026-10-27T08:30:00+10:00")).toBe("08:30")
  })

  it("finds the first day of a month and the grid that holds it", () => {
    expect(monthStartOf("2026-10-27")).toBe("2026-10-01")
    expect(addMonths("2026-10-01", 3)).toBe("2027-01-01")
    expect(addMonths("2026-01-01", -1)).toBe("2025-12-01")
    expect(monthGridStart("2026-10-01")).toBe("2026-09-28")
    expect(monthGridStart("2026-10-01", true)).toBe("2026-09-27")
  })

  it("reads the last day of a whole-day event from its midnight end and lists it on each day it covers", () => {
    expect(lastDayOfWallClock("2026-11-02T00:00", "2026-11-04T00:00")).toBe("2026-11-03")
    expect(lastDayOfWallClock("2026-11-02T09:00", "2026-11-02T10:00")).toBe("2026-11-02")
    const event = {
      id: 1, title: "Выставка", starts_at: "2026-11-02T00:00:00", ends_at: "2026-11-04T00:00:00", location: "",
      notes: "", project_id: 0, client_id: 0, shared: true, sync_push: false, imported: false, all_day: true,
      participant_ids: [], created_by: 1,
    } as CalendarEvent
    expect(daysInWindow(event, "2026-11-01", "2026-11-30")).toEqual(["2026-11-02", "2026-11-03"])
    expect(daysInWindow(event, "2026-11-03", "2026-11-03")).toEqual(["2026-11-03"])
  })
})
