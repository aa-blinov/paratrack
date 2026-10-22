import { describe, expect, it } from "vitest"
import type { InvoiceSummary } from "@/dashboard/types"
import { filterInvoices } from "./invoice-filter"

const all = { status: "all", issuedFrom: "", issuedTo: "", unpaidOnly: false }

function invoice(id: number, status: string, issued: string, overdue = false): InvoiceSummary {
  return { ID: id, Number: `INV-${id}`, Client: "Acme", Status: status, Total: "", Hours: "", Period: "", Due: "", Overdue: overdue, Issued: issued, Partial: "" }
}

const items = [
  invoice(1, "draft", "2026-10-01"),
  invoice(2, "sent", "2026-10-05", true),
  invoice(3, "sent", "2026-10-09"),
  invoice(4, "paid", "2026-10-12"),
]

describe("invoice list filters", () => {
  it("keeps everything with no filter", () => {
    expect(filterInvoices(items, all).map(i => i.ID)).toEqual([1, 2, 3, 4])
  })

  it("filters by status and reads overdue from the server's mark", () => {
    expect(filterInvoices(items, { ...all, status: "sent" }).map(i => i.ID)).toEqual([2, 3])
    expect(filterInvoices(items, { ...all, status: "overdue" }).map(i => i.ID)).toEqual([2])
  })

  it("leaves paid invoices out when only unpaid ones are wanted", () => {
    expect(filterInvoices(items, { ...all, unpaidOnly: true }).map(i => i.ID)).toEqual([1, 2, 3])
  })

  it("filters by the issue day, both ends inclusive", () => {
    expect(filterInvoices(items, { ...all, issuedFrom: "2026-10-05", issuedTo: "2026-10-09" }).map(i => i.ID)).toEqual([2, 3])
  })
})
