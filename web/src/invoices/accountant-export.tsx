import { Download } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { DateField } from "@/components/date-field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"

// The accountant's download (FIN-05): the lines of the issued invoices for one client
// and a period of invoice dates. A plain GET form: the browser saves the CSV and the
// page stays where it is.
export function AccountantExport({ lang, clients }: { lang: string; clients: string[] }) {
  return <Card>
    <CardHeader><CardTitle>{t(lang, "inv.exportTitle")}</CardTitle><CardDescription>{t(lang, "inv.exportHint")}</CardDescription></CardHeader>
    <CardContent>
      <form method="get" action="/invoices/export.csv" className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5">
          <Label htmlFor="export-from" className="text-xs">{t(lang, "inv.exportFrom")}</Label>
          <DateField id="export-from" name="export_from" />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="export-to" className="text-xs">{t(lang, "inv.exportTo")}</Label>
          <DateField id="export-to" name="export_to" />
        </div>
        <div className="grid min-w-0 gap-1.5">
          <Label htmlFor="export-client" className="text-xs">{t(lang, "inv.exportClient")}</Label>
          <Input id="export-client" name="export_client" list="export-clients" placeholder={t(lang, "inv.exportAllClients")} className="min-w-0 sm:w-64" />
          <datalist id="export-clients">{clients.map(name => <option key={name} value={name} />)}</datalist>
        </div>
        <label className="grid gap-1 text-xs">{t(lang, "inv.exportFormat")}
          <select name="format" defaultValue="excel" className="h-9 rounded-md border bg-background px-2 text-sm">
            <option value="excel">{t(lang, "inv.exportExcel")}</option>
            <option value="1c">{t(lang, "inv.export1C")}</option>
            <option value="">CSV</option>
          </select>
        </label>
        <Button type="submit" variant="outline" size="sm"><Download aria-hidden="true" />{t(lang, "inv.exportDownload")}</Button>
        {/* The same period and client, the payments received instead of the
            lines invoiced: what is set against the bank statement. */}
        <Button type="submit" variant="outline" size="sm" formAction="/invoices/payments.csv"><Download aria-hidden="true" />{t(lang, "inv.exportPayments")}</Button>
      </form>
    </CardContent>
  </Card>
}
