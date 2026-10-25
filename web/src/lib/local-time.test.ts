import { describe, expect, it } from "vitest"
import { localDateTimeValue } from "./local-time"

describe("local date-time value", () => {
  it("writes the wall clock the date-time field holds, zero-padded", () => {
    expect(localDateTimeValue(new Date(2026, 9, 4, 9, 5))).toBe("2026-10-04T09:05")
  })
})
