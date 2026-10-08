import * as React from "react"
import { BarChart3, Download, Settings } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import type { ExportData } from "@/dashboard/types"

export function ExportPage({ data }: { data: ExportData }) {
  const lang = data.Lang || "en"
  const [from, setFrom] = React.useState("")
  const [to, setTo] = React.useState("")
  const [error, setError] = React.useState("")
  function submit(event: React.FormEvent<HTMLFormElement>) {
    if (from && to && from > to) {
      event.preventDefault()
      setError(t(lang, "export.invalidPeriod"))
      return
    }
    setError("")
  }
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "export.title")}</h1><p className="mt-1 text-sm text-muted-foreground max-w-[50ch]">{t(lang, "export.blurb")}</p></header>
    <div className="grid gap-4 lg:grid-cols-2 lg:items-start">
      <Card><CardHeader><CardTitle>{t(lang, "export.sessions")}</CardTitle></CardHeader><CardContent className="grid gap-4"><div className="grid gap-2 text-sm text-muted-foreground"><p>{t(lang, "export.contents")}</p><p>{t(lang, "export.columns")}</p></div>
        <form method="get" action="/api/reports.csv" className="grid gap-4 border-t pt-4" onSubmit={submit}>
          <fieldset className="grid gap-3"><legend className="mb-2 font-semibold">{t(lang, "export.period")}</legend><div className="grid gap-3 sm:grid-cols-2"><div className="grid gap-1.5"><Label htmlFor="export-from">{t(lang, "export.from")}</Label><Input id="export-from" type="date" name="from" value={from} onChange={event => { setFrom(event.target.value); setError("") }} /></div><div className="grid gap-1.5"><Label htmlFor="export-to">{t(lang, "export.to")}</Label><Input id="export-to" type="date" name="to" value={to} onChange={event => { setTo(event.target.value); setError("") }} /></div></div><p className="text-sm text-muted-foreground">{t(lang, "export.periodHint")}</p></fieldset>
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          <div className="flex flex-wrap items-center gap-3"><Button type="submit"><Download aria-hidden="true" />{t(lang, "export.download")}</Button><span className="font-mono text-sm text-muted-foreground">paratrack.csv</span></div>
        </form>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>{t(lang, "export.summary")}</CardTitle></CardHeader><CardContent className="grid justify-items-start gap-3"><p className="text-sm text-muted-foreground">{t(lang, "export.summaryHint")}</p>{data.CanManage === false ? <p className="text-sm text-muted-foreground">{t(lang, "export.reportsRestricted")}</p> : data.ReportsEnabled ? <Button asChild variant="outline"><a href="/reports"><BarChart3 aria-hidden="true" />{t(lang, "export.chooseReport")}</a></Button> : <><p className="text-sm text-muted-foreground">{t(lang, "export.reportsOff")}</p><Button asChild variant="outline"><a href="/settings/sections"><Settings aria-hidden="true" />{t(lang, "export.enableReports")}</a></Button></>}</CardContent></Card>
    </div>
  </main>
}
