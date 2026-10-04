import { Activity, BarChart3, CalendarDays, Download, Folder, Users, Zap } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { translate as t } from "@/i18n"
import type { ReportRunData, ReportsData } from "@/dashboard/types"

const icons = { folder: Folder, activity: Activity, calendar: CalendarDays, zap: Zap, users: Users } as const

export function ReportsPage({ data }: { data: ReportsData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-5xl gap-4">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "rep.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "rep.blurb")}</p></header>
    <div className="grid gap-4 sm:grid-cols-2">{data.Templates.map(template => {
      const Icon = icons[template.Icon as keyof typeof icons] || BarChart3
      return <Card key={template.ID}><CardHeader className="flex-row items-center gap-3 space-y-0"><Icon aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" /><CardTitle className="text-base">{t(lang, `rep.${template.ID}.name`)}</CardTitle></CardHeader><CardContent className="grid gap-4"><p className="text-sm text-muted-foreground">{t(lang, `rep.${template.ID}.blurb`)}</p><form method="get" action="/reports/run" className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end"><input type="hidden" name="id" value={template.ID} /><div className="grid gap-1.5"><Label htmlFor={`report-${template.ID}-from`}>{t(lang, "inv.from")}</Label><Input id={`report-${template.ID}-from`} type="date" name="from" defaultValue={data.DefFrom} /></div><div className="grid gap-1.5"><Label htmlFor={`report-${template.ID}-to`}>{t(lang, "inv.to")}</Label><Input id={`report-${template.ID}-to`} type="date" name="to" defaultValue={data.DefTo} /></div><Button type="submit"><BarChart3 aria-hidden="true" />{t(lang, "rep.run")}</Button></form></CardContent></Card>
    })}</div>
  </main>
}

export function ReportRunPage({ data }: { data: ReportRunData }) {
  const lang = data.Lang || "en"
  const report = data.VM
  const csv = `/reports/run?id=${encodeURIComponent(report.Template.id)}&from=${encodeURIComponent(report.From)}&to=${encodeURIComponent(report.To)}&format=csv`
  return <main className="mx-auto grid w-full max-w-5xl gap-4">
    <div className="no-print flex flex-wrap items-center justify-between gap-3 text-sm"><div><a href="/reports" className="text-muted-foreground underline-offset-4 hover:underline">{t(lang, "rep.title")}</a><span className="mx-2 text-muted-foreground">/</span><span>{t(lang, `rep.${report.Template.id}.name`)}</span></div><div className="flex items-center gap-2"><Button asChild variant="outline" size="sm"><a href={csv}><Download aria-hidden="true" />CSV</a></Button><Button type="button" size="sm" onClick={() => window.print()}><Download aria-hidden="true" />{t(lang, "inv.print")}</Button></div></div>
    <Card className="print:border-0"><CardHeader className="gap-1"><p className="text-xs uppercase tracking-wider text-muted-foreground">{t(lang, "rep.report")}</p><CardTitle className="text-2xl">{t(lang, `rep.${report.Template.id}.name`)}</CardTitle><p className="font-mono text-xs text-muted-foreground">{report.PeriodLabel}</p></CardHeader><CardContent className="grid gap-4"><Separator />
      {report.Rows?.length ? <table className="w-full text-sm"><thead className="hidden sm:table-header-group"><tr className="border-b text-muted-foreground"><th className="p-2 text-left font-medium">{t(lang, "rep.key")}</th><th className="p-2 text-right font-medium">{t(lang, "inv.hours")}</th>{report.Template.billable && <><th className="p-2 text-right font-medium">{t(lang, "inv.rate")}</th><th className="p-2 text-right font-medium">{t(lang, "inv.amount")}</th></>}<th className="p-2 text-right font-medium">{t(lang, "stats.share")}</th></tr></thead><tbody>{report.Rows.map((row, index) => <tr key={`${row.Key}:${index}`} className="grid border-b p-2 last:border-0 sm:table-row sm:p-0"><td className="flex items-center justify-between gap-3 p-1 sm:table-cell sm:p-2">{row.Key}</td><td className="flex items-center justify-between gap-3 p-1 text-right font-mono sm:table-cell sm:p-2"><span className="text-xs text-muted-foreground sm:hidden">{t(lang, "inv.hours")}</span>{row.Hours}</td>{report.Template.billable && <><td className="flex items-center justify-between gap-3 p-1 text-right font-mono sm:table-cell sm:p-2"><span className="text-xs text-muted-foreground sm:hidden">{t(lang, "inv.rate")}</span>{row.Rate}</td><td className="flex items-center justify-between gap-3 p-1 text-right font-mono sm:table-cell sm:p-2"><span className="text-xs text-muted-foreground sm:hidden">{t(lang, "inv.amount")}</span>{row.Amount}</td></>}<td className="flex items-center justify-between gap-3 p-1 text-right font-mono text-muted-foreground sm:table-cell sm:p-2"><span className="text-xs sm:hidden">{t(lang, "stats.share")}</span>{row.Share.toFixed(1)}%</td></tr>)}</tbody><tfoot><tr className="grid border-t-2 p-2 font-semibold sm:table-row sm:p-0"><td className="p-1 sm:p-2">{t(lang, "stats.total")}</td><td className="flex items-center justify-between gap-3 p-1 text-right font-mono sm:table-cell sm:p-2"><span className="text-xs text-muted-foreground sm:hidden">{t(lang, "inv.hours")}</span>{report.Total}</td>{report.Template.billable && <><td className="hidden sm:table-cell" /><td className="flex items-center justify-between gap-3 p-1 text-right font-mono sm:table-cell sm:p-2"><span className="text-xs text-muted-foreground sm:hidden">{t(lang, "inv.amount")}</span>{report.TotalAmount}</td></>}<td className="hidden sm:table-cell" /></tr></tfoot></table> : <div className="py-8 text-center"><p className="font-medium">{t(lang, "rep.empty")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "rep.emptyHint")}</p><Button asChild className="mt-3" size="sm"><a href="/reports"><BarChart3 aria-hidden="true" />{t(lang, "rep.emptyCta")}</a></Button></div>}
      <p className="text-xs text-muted-foreground">{t(lang, "rep.footNote")}</p>
    </CardContent></Card>
  </main>
}
