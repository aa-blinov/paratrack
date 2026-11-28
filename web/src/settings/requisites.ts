// Russian bank and company details still live in one free-text field on the
// server, so the builder reads that text back into fields when the page opens
// and writes it out again. A builder that started empty overwrote everything
// the owner had saved the moment they fixed a single БИК.

export type RfFields = {
  org: string
  inn: string
  kpp: string
  ogrn: string
  bank: string
  bik: string
  corr: string
  acc: string
  contract: string
}

export type RfField = keyof RfFields

export const emptyRf: RfFields = { org: "", inn: "", kpp: "", ogrn: "", bank: "", bik: "", corr: "", acc: "", contract: "" }

const numbered: [RfField, RegExp][] = [
  ["inn", /ИНН\s*(\d+)/i],
  ["kpp", /КПП\s*(\d+)/i],
  ["ogrn", /ОГРН(?:ИП)?(?:\s*\/\s*ОГРНИП)?\s*(\d+)/i],
  ["acc", /р\/с\s*(\d+)/i],
  ["bik", /БИК\s*(\d+)/i],
  ["corr", /к\/с\s*(\d+)/i],
]

/** parseRequisites splits saved text into the builder's fields. Lines it does
 * not recognise come back in `rest`, so writing the text out again keeps them. */
export function parseRequisites(text: string): { fields: RfFields; rest: string[] } {
  const fields = { ...emptyRf }
  const rest: string[] = []
  const lines = text.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
  lines.forEach((line, index) => {
    let matched = false
    for (const [key, re] of numbered) {
      const m = line.match(re)
      if (m && !fields[key]) {
        fields[key] = m[1]
        matched = true
      }
    }
    if (matched) {
      const bank = line.match(/р\/с\s*\d+\s+в\s+(.+)$/i)
      if (bank && !fields.bank) fields.bank = bank[1].trim()
      return
    }
    if (/^договор/i.test(line) && !fields.contract) fields.contract = line
    else if (index === 0 && !fields.org) fields.org = line
    else rest.push(line)
  })
  return { fields, rest }
}

/** buildRequisites writes the fields back as text, followed by the lines the
 * parser did not recognise and then the contract line. */
export function buildRequisites(f: RfFields, rest: string[] = []): string {
  const v = (s: string) => s.trim()
  const lines: string[] = []
  if (v(f.org)) lines.push(v(f.org))
  const ids = [v(f.inn) && `ИНН ${v(f.inn)}`, v(f.kpp) && `КПП ${v(f.kpp)}`, v(f.ogrn) && `ОГРН/ОГРНИП ${v(f.ogrn)}`].filter(Boolean)
  if (ids.length) lines.push(ids.join(", "))
  const account = [v(f.acc) && `р/с ${v(f.acc)}`, v(f.bank) && `в ${v(f.bank)}`].filter(Boolean)
  if (account.length) lines.push(account.join(" "))
  const bankIds = [v(f.bik) && `БИК ${v(f.bik)}`, v(f.corr) && `к/с ${v(f.corr)}`].filter(Boolean)
  if (bankIds.length) lines.push(bankIds.join(", "))
  lines.push(...rest)
  if (v(f.contract)) lines.push(v(f.contract))
  return lines.join("\n")
}

const digits = (s: string) => s.split("").map(Number)

function innValid(inn: string): boolean {
  const d = digits(inn)
  const check = (weights: number[]) => weights.reduce((sum, w, i) => sum + w * d[i], 0) % 11 % 10
  if (d.length === 10) return check([2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[9]
  if (d.length === 12) {
    return check([7, 2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[10] && check([3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[11]
  }
  return false
}

// The control key of a Russian account is checked against the БИК: the
// account with the БИК's last three digits (a settlement account) or "0" and
// its fifth and sixth (a correspondent account) in front, weighted 7-1-3.
function accountKeyValid(prefix: string, account: string): boolean {
  const weights = [7, 1, 3]
  const sum = digits(prefix + account).reduce((acc, d, i) => acc + d * weights[i % 3], 0)
  return sum % 10 === 0
}

/** invalidRequisites lists the filled fields that cannot be right. Empty
 * fields are allowed: not every document needs every number. */
export function invalidRequisites(f: RfFields): RfField[] {
  const bad: RfField[] = []
  const v = (s: string) => s.replace(/\s+/g, "")
  const only = (s: string, ...lengths: number[]) => /^\d+$/.test(s) && lengths.includes(s.length)
  const inn = v(f.inn), kpp = v(f.kpp), ogrn = v(f.ogrn), bik = v(f.bik), corr = v(f.corr), acc = v(f.acc)
  if (inn && !innValid(inn)) bad.push("inn")
  if (kpp && !only(kpp, 9)) bad.push("kpp")
  if (ogrn && !only(ogrn, 13, 15)) bad.push("ogrn")
  const bikOK = only(bik, 9)
  if (bik && !bikOK) bad.push("bik")
  if (acc && (!only(acc, 20) || (bikOK && !accountKeyValid(bik.slice(-3), acc)))) bad.push("acc")
  if (corr && (!only(corr, 20) || (bikOK && !accountKeyValid("0" + bik.slice(4, 6), corr)))) bad.push("corr")
  return bad
}
