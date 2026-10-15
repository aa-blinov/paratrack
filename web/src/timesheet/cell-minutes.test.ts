import { describe, expect, it } from "vitest"
import { cellMinutes } from "./timesheet-page"

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
})
