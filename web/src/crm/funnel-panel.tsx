import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { DateField } from "@/components/date-field"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import { apiJSON, ApiError } from "@/lib/json-api"
import { formatMoney } from "@/lib/money"
import { defaultFunnelPeriod, percentLabel, type FunnelAmount, type FunnelReport, type FunnelTotal } from "@/crm/model"

/**
 * The funnel of a period (CRM-17). The server decides who sees amounts: a total carries them only
 * when amounts_visible is set, so this screen draws them under that flag and never builds one itself.
 */
export function FunnelPanel({ lang }: { lang: string }) {
  const initial = React.useMemo(() => defaultFunnelPeriod(new Date()), [])
  const [from, setFrom] = React.useState(initial.from)
  const [to, setTo] = React.useState(initial.to)
  const [report, setReport] = React.useState<FunnelReport | null>(null)
  const [error, setError] = React.useState("")

  React.useEffect(() => {
    // A date that could not be read arrives as "", and nothing is asked for it.
    if (!from || !to) return
    let current = true
    apiJSON<FunnelReport>(`/api/crm/report/funnel?${new URLSearchParams({ from, to })}`, "GET", "")
      .then(body => { if (current) { setReport(body); setError("") } })
      .catch(cause => { if (current) setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic")) })
    return () => { current = false }
  }, [from, to, lang])

  const showAmounts = report?.amounts_visible === true

  return <Card>
    <CardHeader><CardTitle>{t(lang, "crm.funnel.title")}</CardTitle></CardHeader>
    <CardContent className="grid gap-5">
      <p className="text-sm text-muted-foreground">{t(lang, "crm.funnel.blurb")}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="grid gap-1.5"><Label htmlFor="funnel-from">{t(lang, "crm.funnel.from")}</Label><DateField id="funnel-from" name="from" defaultValue={from} onValueChange={setFrom} /></div>
        <div className="grid gap-1.5"><Label htmlFor="funnel-to">{t(lang, "crm.funnel.to")}</Label><DateField id="funnel-to" name="to" defaultValue={to} onValueChange={setTo} /></div>
      </div>
      {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
      {!from || !to ? <p className="text-sm text-muted-foreground">{t(lang, "crm.funnel.badPeriod")}</p> : report && <>
        <section aria-label={t(lang, "crm.funnel.pipeline")} className="grid gap-2">
          <h3 className="text-sm font-semibold">{t(lang, "crm.funnel.pipeline")}</h3>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-[16rem] border-collapse text-sm">
              <thead><tr className="border-b text-left text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t(lang, "crm.stageLabel")}</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "crm.funnel.deals")}</th>
                {showAmounts && <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "crm.funnel.amount")}</th>}
              </tr></thead>
              <tbody>{report.pipeline.map(stage => <tr key={stage.stage} className="border-b last:border-0">
                <th scope="row" className="px-3 py-2 text-left font-medium">{stage.name}</th>
                <td className="px-3 py-2 text-right tabular-nums">{stage.count}</td>
                {showAmounts && <td className="px-3 py-2 text-right"><AmountLines amounts={stage.amounts} lang={lang} /></td>}
              </tr>)}</tbody>
            </table>
          </div>
        </section>

        <section aria-label={t(lang, "crm.funnel.period")} className="grid gap-3">
          <h3 className="text-sm font-semibold">{t(lang, "crm.funnel.period")}</h3>
          <div className="grid gap-4 sm:grid-cols-3">
            <FunnelTile label={t(lang, "crm.funnel.won")} total={report.won} showAmounts={showAmounts} lang={lang} />
            <FunnelTile label={t(lang, "crm.funnel.lost")} total={report.lost} showAmounts={showAmounts} lang={lang} />
            <FunnelTile label={t(lang, "crm.funnel.overdue")} total={report.overdue} showAmounts={showAmounts} lang={lang} />
          </div>
          <p className="text-sm">{report.conversion === null ? t(lang, "crm.funnel.noClosed") : t(lang, "crm.funnel.conversion", percentLabel(report.conversion))}</p>
        </section>

        <section aria-label={t(lang, "crm.funnel.leads")} className="grid gap-3">
          <h3 className="text-sm font-semibold">{t(lang, "crm.funnel.leads")}</h3>
          <h4 className="text-sm font-medium">{t(lang, "crm.funnel.leadsNow")}</h4>
          <div className="flex flex-wrap gap-2">
            {report.leads.statuses.map(item => <Badge key={item.status} variant="secondary">{t(lang, `crm.leadStatus.${item.status}`)}: {item.count}</Badge>)}
          </div>
          {report.leads.created === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "crm.funnel.noLeads")}</p> : <>
            <div className="grid gap-4 sm:grid-cols-3">
              <div className="grid gap-1"><span className="text-xs text-muted-foreground">{t(lang, "crm.funnel.leadsCreated")}</span><span className="font-mono text-2xl tabular-nums">{report.leads.created}</span></div>
              <div className="grid gap-1"><span className="text-xs text-muted-foreground">{t(lang, "crm.funnel.leadsConverted")}</span><span className="font-mono text-2xl tabular-nums">{report.leads.converted}</span></div>
              <p className="self-end text-sm">{report.leads.conversion === null ? "" : t(lang, "crm.funnel.leadConversion", percentLabel(report.leads.conversion))}</p>
            </div>
            <div className="grid gap-2">
              <h4 className="text-sm font-medium">{t(lang, "crm.funnel.sources")}</h4>
              <ul className="grid divide-y rounded-lg border">
                {report.leads.sources.map(item => <li key={item.source} className="flex justify-between gap-3 px-3 py-2 text-sm">
                  <span className="[overflow-wrap:anywhere]">{item.label}</span><span className="font-mono tabular-nums">{item.count}</span>
                </li>)}
              </ul>
            </div>
          </>}
          <div className="grid gap-1 text-sm">
            <h4 className="font-medium">{t(lang, "crm.funnel.response")}</h4>
            <p className="text-muted-foreground">{t(lang, "crm.funnel.responseTarget", report.response.target_minutes)}</p>
            <p>{t(lang, "crm.funnel.responseLine", report.response.within_target, report.response.created)}</p>
            <p>{report.response.median_minutes === null ? t(lang, "crm.funnel.noReply") : t(lang, "crm.funnel.median", report.response.median_minutes)}</p>
          </div>
        </section>
      </>}
    </CardContent>
  </Card>
}

function FunnelTile({ label, total, showAmounts, lang }: { label: string; total: FunnelTotal; showAmounts: boolean; lang: string }) {
  return <div className="grid content-start gap-1">
    <span className="text-xs text-muted-foreground">{label}</span>
    <span className="font-mono text-2xl tabular-nums">{total.count}</span>
    {showAmounts && <AmountLines amounts={total.amounts} lang={lang} />}
  </div>
}

// Each currency keeps its own line, so two currencies never read as one sum.
function AmountLines({ amounts, lang }: { amounts?: FunnelAmount[]; lang: string }) {
  return <>{(amounts ?? []).map(item => <span key={item.currency} className="block font-mono tabular-nums">{formatMoney(item.amount_cents, item.currency, lang)}</span>)}</>
}
