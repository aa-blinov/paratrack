import { describe, expect, it } from "vitest"
import { encodeRequestBody, isValidQueueItem, readQueue, sameOriginPath, writeQueue, type QueueItem } from "./queue"

function memoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => { data.set(key, value) },
  } as unknown as Storage
}

describe("offline queue", () => {
  it("treats corrupt or unavailable storage as empty and reports write failure", () => {
    expect(readQueue({ getItem: () => "{broken" } as unknown as Storage)).toEqual([])
    const storage = memoryStorage()
    const queue: QueueItem[] = [{ verb: "POST", url: "/api/start", encoding: "form", body: [] }]
    expect(writeQueue(storage, queue)).toBe(true)
    expect(readQueue(storage)).toEqual(queue)
    expect(writeQueue({ setItem() { throw new Error("quota") } } as unknown as Storage, queue)).toBe(false)
  })

  it("requires a supported body shape", () => {
    expect(isValidQueueItem({ verb: "POST", url: "/api/start", encoding: "form", body: [["activity", "work"]], ts: 1 })).toBe(true)
    expect(isValidQueueItem({ verb: "POST", url: "/api/start", encoding: "json", body: { activity: "work" } })).toBe(true)
    expect(isValidQueueItem({ verb: "POST", url: "/api/start", encoding: "json", body: ["work"] })).toBe(false)
    expect(isValidQueueItem({ verb: "POST", url: "/api/start", encoding: "form", body: [["x", 1]] })).toBe(false)
  })

  it("restricts replays to the page origin", () => {
    const page = "https://app.example/stats"
    expect(sameOriginPath("/api/start", page)).toBe("/api/start")
    expect(sameOriginPath("https://app.example/api/start?q=1", page)).toBe("/api/start?q=1")
    expect(sameOriginPath("https://evil.example/api/start", page)).toBeNull()
  })

  it("encodes forms and accepts JSON objects only", () => {
    expect(encodeRequestBody(new URLSearchParams([["tag", "one"], ["tag", "two"]]), "application/x-www-form-urlencoded"))
      .toEqual({ body: [["tag", "one"], ["tag", "two"]], encoding: "form" })
    expect(encodeRequestBody('{"enabled":true}', "application/problem+json"))
      .toEqual({ body: { enabled: true }, encoding: "json" })
    expect(encodeRequestBody("[1,2]", "application/json")).toBeNull()
  })
})
