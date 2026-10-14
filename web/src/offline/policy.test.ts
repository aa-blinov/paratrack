import { describe, expect, it } from "vitest"
import { isOfflineQueueableMutation } from "./policy"

describe("offline replay policy", () => {
  it("allows timer, schedule and ledger mutations", () => {
    for (const [method, path] of [
      ["POST", "/api/start"],
      ["POST", "/api/sessions/42/stop"],
      ["POST", "/api/timesheet/cell"],
      ["PATCH", "/api/sessions/42"],
      ["DELETE", "/api/sessions/42/tags"],
    ]) {
      expect(isOfflineQueueableMutation(method, path), `${method} ${path}`).toBe(true)
    }
  })

  it("keeps credential and payment mutations online-only", () => {
    for (const [method, path] of [
      ["POST", "/api/login"],
      ["POST", "/api/profile/password"],
      ["POST", "/api/integrations"],
      ["POST", "/api/team/stripe"],
      ["POST", "/api/push/subscribe"],
      ["POST", "/api/invoices/12/paid"],
    ]) {
      expect(isOfflineQueueableMutation(method, path), `${method} ${path}`).toBe(false)
    }
  })

  it("rejects non-mutating methods and malformed session paths", () => {
    expect(isOfflineQueueableMutation("GET", "/api/start")).toBe(false)
    expect(isOfflineQueueableMutation("POST", "/api/sessions/not-an-id/stop")).toBe(false)
    expect(isOfflineQueueableMutation("PUT", "/api/start")).toBe(false)
  })
})
