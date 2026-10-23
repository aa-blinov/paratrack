import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { DateField } from "@/components/date-field"
import { Label } from "@/components/ui/label"
import type { InvoicesData } from "@/dashboard/types"
import { translate as t } from "@/i18n"

type Reconcile = NonNullable<InvoicesData["Reconcile"]>

// The reconciliation (FIN-04): for a client and a window of days, the time worked against
// what the invoices bill and what no invoice holds yet. A row whose difference is not zero
// says so in words as well as colour. The form has no action, so it reloads this page.
export function ReconcileCard({ lang, reconcile }: { lang: string; reconcile?: Reconcile | null }) {
  return <Card>
    <CardHeader><CardTitle>{t(lang, "fin.reconcileTitle")}</CardTitle><CardDescription>{t(lang, "fin.reconcileBlurb")}</CardDescription></CardHeader>
    <CardContent className="grid gap-4">
      <form method="get" className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5"><Label htmlFor="reconcile-from" className="text-xs">{t(lang, "fin.reconcileFrom")}</Label><DateField id="reconcile-from" name="reconcile_from" defaultValue={reconcile?.From} required /></div>
        <div className="grid gap-1.5"><Label htmlFor="reconcile-to" className="text-xs">{t(lang, "fin.reconcileTo")}</Label><DateField id="reconcile-to" name="reconcile_to" defaultValue={reconcile?.To} required /></div>
        <Button type="submit" variant="outline" size="sm">{t(lang, "fin.reconcileShow")}</Button>
      </form>
      {reconcile?.Note && <p role="alert" className="text-sm text-destructive">{reconcile.Note}</p>}
      {reconcile && !reconcile.Note && (reconcile.Rows.length === 0
        ? <p className="text-sm text-muted-foreground">{t(lang, "fin.reconcileEmpty")}</p>
        : <div className="overflow-x-auto">
          <table className="w-full min-w-[36rem] border-collapse text-sm">
            <thead><tr className="border-b text-left text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "fin.reconcileClient")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "fin.reconcileWorked")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "fin.reconcileBilled")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "fin.reconcileNotBilled")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "fin.reconcileDiff")}</th>
            </tr></thead>
            <tbody>{reconcile.Rows.map(row => <tr key={row.Client} className="border-b last:border-0">
              <th scope="row" className="px-3 py-2 text-left font-medium">{row.Client}</th>
              <td className="px-3 py-2 text-right tabular-nums">{row.Worked}</td>
              <td className="px-3 py-2 text-right tabular-nums">{row.Billed}</td>
              <td className="px-3 py-2 text-right tabular-nums">{row.NotBilled}</td>
              <td data-mismatch={row.Mismatched || undefined} className={`px-3 py-2 text-right tabular-nums ${row.Mismatched ? "text-destructive" : ""}`}>
                {row.Difference}
                {row.Mismatched && <span className="block text-xs font-normal">{t(lang, "fin.reconcileMismatch")}</span>}
              </td>
            </tr>)}</tbody>
          </table>
        </div>)}
    </CardContent>
  </Card>
}
