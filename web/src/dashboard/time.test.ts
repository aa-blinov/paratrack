import { describe, expect, it } from "vitest"
import { clockLabel, durationLabel, sessionElapsedSeconds } from "./time"
import type { Session } from "./types"

const session = (overrides: Partial<Session> = {}): Session => ({
  ID: 1, ActivityID: 1, ActivityName: "Writing", Color: "#000", ProjectID: 0,
  ProjectName: "", ProjectColor: "", ProjectSlug: "", StartISO: "", ResumeISO: "",
  Clock: "00:00:00", StartLocal: "", EndLocal: "", Duration: "", DurationSecs: 0,
  DurationInput: "", AccumulatedSeconds: 0, Paused: false, Note: "", Tags: [], ...overrides,
})

describe("dashboard timer formatting", () => {
  it("formats a clock with hours beyond one day", () => {
    expect(clockLabel(90_061)).toBe("25:01:01")
  })

  it("adds time since the latest resume and never runs backwards", () => {
    const running = session({ Clock: "00:03:00", ResumeISO: "2026-01-01T00:00:00Z" })
    expect(sessionElapsedSeconds(running, Date.parse("2026-01-01T00:00:12Z"))).toBe(192)
    expect(sessionElapsedSeconds(running, Date.parse("2025-12-31T23:59:00Z"))).toBe(180)
  })

  it("freezes paused clocks and falls back on invalid resume timestamps", () => {
    expect(sessionElapsedSeconds(session({ Clock: "00:01:30", Paused: true }), Date.now())).toBe(90)
    expect(sessionElapsedSeconds(session({ Clock: "00:01:30", ResumeISO: "invalid" }), Date.now())).toBe(90)
  })

  it("uses localized compact duration labels", () => {
    expect(durationLabel(90_061, "en")).toBe("1d 1h")
    expect(durationLabel(3_661, "ru")).toBe("1ч 1м")
    expect(durationLabel(-1, "en")).toBe("0s")
  })
})
