import { describe, expect, it } from "vitest"
import { cellClock, cellMinutes } from "./timesheet-page"

describe("timesheet cell input", () => {
  it("reads minutes and hours:minutes", () => {
    expect(cellMinutes("90")).toBe(90)
    expect(cellMinutes("1:30")).toBe(90)
    expect(cellMinutes(" 0:05 ")).toBe(5)
    expect(cellMinutes("")).toBe(0)
  })
  it("refuses what it cannot read", () => {
    for (const bad of ["1:3", "1:75", "1,5", "-5", "abc"]) expect(cellMinutes(bad)).toBeNaN()
  })
  it("shows a saved value the way it can be typed", () => {
    expect(cellClock(90)).toBe("1:30")
    expect(cellClock(45)).toBe("0:45")
    expect(cellClock(480)).toBe("8:00")
    expect(cellMinutes(cellClock(135))).toBe(135)
  })
})
