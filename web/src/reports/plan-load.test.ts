import { describe, expect, it } from "vitest"
import { planLoad } from "./plan-load"

describe("load of a row against its plan", () => {
  it("is the tracked share of the plan", () => {
    expect(planLoad(12 * 3600, 30 * 3600)).toBeCloseTo(40)
  })

  it("passes 100 when more was tracked than planned", () => {
    expect(planLoad(36 * 3600, 30 * 3600)).toBeCloseTo(120)
  })

  it("is zero, not empty, when there is a plan and nothing is tracked yet", () => {
    expect(planLoad(0, 30 * 3600)).toBe(0)
  })

  it("is empty when there is no plan to measure against", () => {
    expect(planLoad(12 * 3600, 0)).toBeNull()
    expect(planLoad(0, 0)).toBeNull()
  })
})
