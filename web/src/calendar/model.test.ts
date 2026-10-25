import { describe, expect, it } from "vitest"
import { addDays, addMinutes, clockOf, nextFullHour, weekStartOf } from "./model"

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
})
