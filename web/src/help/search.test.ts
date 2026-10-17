import { describe, expect, it } from "vitest"
import { matchesHelpQuery, normalizeHelpText } from "./search"

describe("help search", () => {
  it("matches everything for an empty query", () => {
    expect(matchesHelpQuery("Счета клиентам", "")).toBe(true)
    expect(matchesHelpQuery("Счета клиентам", "   ")).toBe(true)
  })

  it("ignores case and treats ё as е", () => {
    expect(matchesHelpQuery("Счёт клиенту", "СЧЕТ")).toBe(true)
    expect(normalizeHelpText("Ёлка")).toBe("елка")
  })

  it("needs every word of the query, in any order", () => {
    const entry = "Выплаты участникам за период по почасовой ставке"
    expect(matchesHelpQuery(entry, "ставке выплаты")).toBe(true)
    expect(matchesHelpQuery(entry, "выплаты счёт")).toBe(false)
  })

  it("matches a part of a word, so a prefix finds the entry", () => {
    expect(matchesHelpQuery("Приглашения участников", "приглаш")).toBe(true)
  })
})
