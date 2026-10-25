import { describe, expect, it } from "vitest"
import { centsInput, parseMoneyCents } from "./money"

// The amount goes to the server as integer cents. A typed comma or a stray
// letter must never turn into a different number of cents.

describe("money typed in units, sent in cents", () => {
  it("reads a decimal comma and a decimal point the same way", () => {
    expect(parseMoneyCents("1500,5")).toBe(150050)
    expect(parseMoneyCents("1500.50")).toBe(150050)
    expect(parseMoneyCents(" 1 200 ")).toBe(120000)
  })

  it("treats an empty amount as no amount and refuses what it cannot read", () => {
    expect(parseMoneyCents("")).toBe(0)
    expect(parseMoneyCents("1.234")).toBeNull()
    expect(parseMoneyCents("-5")).toBeNull()
    expect(parseMoneyCents("12abc")).toBeNull()
  })

  it("shows an edited amount back in units, leaving zero empty", () => {
    expect(centsInput(150050)).toBe("1500.5")
    expect(centsInput(0)).toBe("")
  })
})
