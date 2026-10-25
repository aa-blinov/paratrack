import { describe, expect, it } from "vitest"
import { intakeExample, leadIsOpen, type Lead } from "@/crm/model"

const lead = (status: Lead["status"]) => ({ status }) as Lead

describe("lead model", () => {
  it("keeps converted and lost leads closed", () => {
    expect(leadIsOpen(lead("new"))).toBe(true)
    expect(leadIsOpen(lead("qualified"))).toBe(true)
    expect(leadIsOpen(lead("converted"))).toBe(false)
    expect(leadIsOpen(lead("lost"))).toBe(false)
  })

  it("writes the intake example with the key in the header and the page's origin", () => {
    const text = intakeExample("https://crm.example.test", "lk_abc")
    expect(text.startsWith("curl -X POST https://crm.example.test/api/leads/intake")).toBe(true)
    expect(text).toContain('-H "X-Intake-Key: lk_abc"')
  })
})
