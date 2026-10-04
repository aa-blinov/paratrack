import * as React from "react"
import { ArrowLeft, Play } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import type { GraphData } from "@/dashboard/types"

const periods = ["today", "yesterday", "week", "last_week", "month", "last_month"] as const

function queryFor(data: GraphData, period = data.Period.Label, keepFilters = true) {
  const query = new URLSearchParams({ period })
  if (period === "custom") {
    query.set("start", data.PeriodStartInput)
    query.set("end", data.PeriodEndInput)
  }
  if (keepFilters) {
    if (data.ProjectFilter) query.set("project", data.ProjectFilter)
    if (data.TagFilter) query.set("tag", data.TagFilter)
    if (data.PersonFilter) query.set("person", String(data.PersonFilter))
  }
  return `?${query}`
}

function ChartRuntime() {
  React.useEffect(() => {
    const ready = () => document.dispatchEvent(new Event("paratrack:graph-ready"))
    if ("echarts" in window) { ready(); return }
    const existing = document.querySelector<HTMLScriptElement>('script[data-paratrack-react-echarts]')
    if (existing) {
      existing.addEventListener("load", ready, { once: true })
      return () => existing.removeEventListener("load", ready)
    }
    const script = document.createElement("script")
    script.src = "/static/js/echarts.min.js"
    script.async = true
    script.dataset.paratrackReactEcharts = "true"
    script.addEventListener("load", ready, { once: true })
    document.head.appendChild(script)
    return () => script.removeEventListener("load", ready)
  }, [])
  return null
}

export function GraphPage({ data }: { data: GraphData }) {
  const lang = data.Lang
  const filtered = Boolean(data.ProjectFilter || data.TagFilter || data.PersonFilter)
  const canShowGraph = data.Chart.HasData

  return <main className="mx-auto w-full max-w-6xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "graph.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "graph.pageBlurb")}</p></header>
    <a href={`/stats${queryFor(data)}`} className="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"><ArrowLeft aria-hidden="true" className="size-4" />{t(lang, "nav.stats")}</a>

    <nav aria-label={t(lang, "goals.period")} className="period-tabs flex flex-wrap gap-1 rounded-lg border bg-card p-1">
      {periods.map(period => <Button key={period} asChild size="sm" variant={data.Period.Label === period ? "secondary" : "ghost"} aria-current={data.Period.Label === period ? "page" : undefined}>
        <a href={queryFor(data, period)}>{t(lang, period === "today" ? "dash.today" : `stats.${period === "last_week" ? "lastWeek" : period === "last_month" ? "lastMonth" : period}`)}</a>
      </Button>)}
    </nav>

    {filtered && <div role="status" className="flex flex-wrap items-center gap-3 rounded-lg border bg-card p-3 text-sm">
      <span className="text-muted-foreground">{t(lang, "graph.scope")}</span>{data.ProjectFilter && <strong>{data.ProjectName}</strong>}{data.TagFilter && <strong>#{data.TagFilter}</strong>}{data.PersonFilter > 0 && <strong>{data.PersonName}</strong>}
      <Button asChild variant="ghost" size="sm" className="ms-auto"><a href={queryFor(data, data.Period.Label, false)}>{t(lang, "stats.clearFilters")}</a></Button>
    </div>}

    <Card>
      <CardHeader><CardTitle>{t(lang, "graph.subtitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "graph.blurbFull")}</p></CardHeader>
      <CardContent>
        {canShowGraph ? <>
          <div className="echart-wrap min-w-0" id="echart-wrap" data-chart={data.ChartJSON}>
            <div className="relative min-w-0">
              <div id="echart-skeleton" className="sk-chart" aria-hidden="true"><span style={{ height: "38%" }} /><span style={{ height: "62%" }} /><span style={{ height: "48%" }} /><span style={{ height: "78%" }} /><span style={{ height: "55%" }} /><span style={{ height: "70%" }} /><span style={{ height: "42%" }} /></div>
              <div id="echart-canvas" className="echart-canvas w-full" style={{ height: 380 }} role="img" aria-label={t(lang, "graph.subtitle")} />
            </div>
          </div>
          <p className="mt-3 text-sm"><span className="text-muted-foreground">{t(lang, "graph.totalTracked")} </span><strong className="font-mono tabular-nums">{data.Chart.TotalLabel}</strong></p>
          <ChartRuntime />
        </> : <div className="py-8 text-center">
          <p className="font-medium">{t(lang, "graph.noData")}</p>
          {filtered ? <><p className="mt-1 text-sm text-muted-foreground">{t(lang, "graph.noDataFiltered")}</p><Button asChild variant="ghost" size="sm" className="mt-3"><a href={queryFor(data, data.Period.Label, false)}>{t(lang, "stats.clearFilters")}</a></Button></> : <><p className="mt-1 text-sm text-muted-foreground">{t(lang, "graph.noDataHint")}</p><Button asChild size="sm" className="mt-3"><a href="/"><Play aria-hidden="true" />{t(lang, "graph.noDataCta")}</a></Button></>}
        </div>}
      </CardContent>
    </Card>

    {canShowGraph && <Card><CardHeader><CardTitle>{t(lang, "graph.legend")}</CardTitle></CardHeader><CardContent><div className="flex flex-wrap gap-2" id="legend-chips">
      {data.Chart.Legend.map((item, index) => <button key={`${item.Name}:${index}`} type="button" className="legend-chip inline-flex max-w-full items-center gap-2 rounded-full border px-3 py-1.5 text-sm hover:bg-muted" data-series-index={index} aria-pressed="true" title={t(lang, "graph.toggleSeries", item.Name)}>
        <span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: item.Color }} /><span className="truncate">{item.Name}</span>
      </button>)}
    </div></CardContent></Card>}
  </main>
}
