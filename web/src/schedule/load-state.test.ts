import { describe, expect, it } from "vitest"
import { loadState } from "./load-state"

const hour = 60

describe("load note of a person's week", () => {
  it("is idle just under half of the available time", () => {
    expect(loadState(19 * hour, 40 * hour)).toBe("idle")
  })

  it("is not idle at exactly half of the available time", () => {
    expect(loadState(20 * hour, 40 * hour)).toBeNull()
  })

  it("is idle at a plan of 15 hours out of 40", () => {
    expect(loadState(15 * hour, 40 * hour)).toBe("idle")
  })

  it("is idle with nothing planned while time is available", () => {
    expect(loadState(0, 40 * hour)).toBe("idle")
  })

  it("is overload when the plan is over the available time, and never idle", () => {
    expect(loadState(45 * hour, 40 * hour)).toBe("overload")
  })

  it("has no note without available time and without a plan", () => {
    expect(loadState(0, 0)).toBeNull()
  })

  it("is overload without available time when anything is planned", () => {
    expect(loadState(hour, 0)).toBe("overload")
  })
})
