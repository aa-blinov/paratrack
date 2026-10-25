import { describe, expect, it } from "vitest"
import { defaultFunnelPeriod, intakeExample, leadIsOpen, percentLabel, responseState, type Lead } from "@/crm/model"

const lead = (status: Lead["status"]) => ({ status }) as Lead

describe("lead model", () => {
  it("keeps converted and lost leads closed", () => {
    expect(leadIsOpen(lead("new"))).toBe(true)
    expect(leadIsOpen(lead("qualified"))).toBe(true)
    expect(leadIsOpen(lead("converted"))).toBe(false)
    expect(leadIsOpen(lead("lost"))).toBe(false)
  })

  it("reads the first response as in time, late or waiting", () => {
    const answered = (fields: Partial<Lead>) => responseState({ within_target: false, response_minutes: 0, waiting_minutes: 0, ...fields } as Lead)
    expect(answered({ within_target: true, response_minutes: 90 })).toEqual({ kind: "inTime", minutes: 90 })
    expect(answered({ within_target: true, response_minutes: 0 })).toEqual({ kind: "inTime", minutes: 0 })
    expect(answered({ within_target: false, response_minutes: 180 })).toEqual({ kind: "late", minutes: 180 })
    expect(answered({ within_target: false, response_minutes: 0, waiting_minutes: 45 })).toEqual({ kind: "waiting", minutes: 45 })
  })

  it("writes the intake example with the key in the header and the page's origin", () => {
    const text = intakeExample("https://crm.example.test", "lk_abc")
    expect(text.startsWith("curl -X POST https://crm.example.test/api/leads/intake")).toBe(true)
    expect(text).toContain('-H "X-Intake-Key: lk_abc"')
  })
})

describe("funnel model", () => {
  it("opens the period on the first day of the month up to today", () => {
    expect(defaultFunnelPeriod(new Date(2026, 9, 25, 14, 30))).toEqual({ from: "2026-10-01", to: "2026-10-25" })
    expect(defaultFunnelPeriod(new Date(2026, 0, 1))).toEqual({ from: "2026-01-01", to: "2026-01-01" })
  })

  it("prints a rate as a whole percentage", () => {
    expect(percentLabel(0.25)).toBe("25%")
    expect(percentLabel(1 / 3)).toBe("33%")
    expect(percentLabel(1)).toBe("100%")
  })
})
