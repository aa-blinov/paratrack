import type { InvoiceSummary } from "@/dashboard/types"

export type InvoiceFilters = {
  status: string
  issuedFrom: string
  issuedTo: string
  unpaidOnly: boolean
}

// The list filters on what the page already has (FIN-09). "overdue" is the
// server's own mark on a sent invoice, not a status of its own, so it is read
// from Overdue. Dates are ISO days, so a plain string comparison is a date one.
export function filterInvoices(items: InvoiceSummary[], filters: InvoiceFilters): InvoiceSummary[] {
  return items.filter(item => {
    if (filters.status === "overdue" && !item.Overdue) return false
    if (filters.status !== "all" && filters.status !== "overdue" && item.Status !== filters.status) return false
    if (filters.unpaidOnly && item.Status === "paid") return false
    if (filters.issuedFrom && item.Issued < filters.issuedFrom) return false
    if (filters.issuedTo && item.Issued > filters.issuedTo) return false
    return true
  })
}
