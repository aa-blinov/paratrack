import { describe, expect, it } from "vitest"
import { buildRequisites, emptyRf, invalidRequisites, parseRequisites } from "./requisites"

// Sberbank's public details: a real БИК with its correspondent account, so the
// control keys are checked against numbers a bank actually issued.
const sber = { bik: "044525225", corr: "30101810400000000225" }

describe("requisites", () => {
  it("reads saved text back into fields and keeps the lines it does not know", () => {
    const saved = [
      "ИП Иванов Иван Иванович",
      "ИНН 7707083893, ОГРН/ОГРНИП 1027700132195",
      "р/с 40702810938000000001 в ПАО Сбербанк",
      "БИК 044525225, к/с 30101810400000000225",
      "Адрес: Москва, ул. Вавилова, 19",
      "Договор № 7 от 01.10.2026",
    ].join("\n")
    const { fields, rest } = parseRequisites(saved)
    expect(fields.org).toBe("ИП Иванов Иван Иванович")
    expect(fields.inn).toBe("7707083893")
    expect(fields.ogrn).toBe("1027700132195")
    expect(fields.acc).toBe("40702810938000000001")
    expect(fields.bank).toBe("ПАО Сбербанк")
    expect(fields.bik).toBe("044525225")
    expect(fields.corr).toBe("30101810400000000225")
    expect(fields.contract).toBe("Договор № 7 от 01.10.2026")
    expect(rest).toEqual(["Адрес: Москва, ул. Вавилова, 19"])
    expect(buildRequisites(fields, rest)).toBe(saved)
  })

  it("changing one field keeps everything else", () => {
    const { fields, rest } = parseRequisites("ООО Ромашка\nИНН 7707083893\nАдрес: Казань")
    const text = buildRequisites({ ...fields, bik: sber.bik }, rest)
    expect(text).toBe("ООО Ромашка\nИНН 7707083893\nБИК 044525225\nАдрес: Казань")
  })

  it("checks the ИНН and the account control keys", () => {
    expect(invalidRequisites({ ...emptyRf, inn: "7707083893", ...sber })).toEqual([])
    expect(invalidRequisites({ ...emptyRf, inn: "7707083894" })).toEqual(["inn"])
    expect(invalidRequisites({ ...emptyRf, inn: "500100732259" })).toEqual([])
    expect(invalidRequisites({ ...emptyRf, bik: sber.bik, corr: "30101810400000000226" })).toEqual(["corr"])
    expect(invalidRequisites({ ...emptyRf, kpp: "12345", bik: "1234" })).toEqual(["kpp", "bik"])
    expect(invalidRequisites(emptyRf)).toEqual([])
  })
})
