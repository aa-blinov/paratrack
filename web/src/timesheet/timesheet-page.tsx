import * as React from "react"
import { ArrowLeft, ArrowRight, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { TimesheetData, TimesheetRow } from "@/dashboard/types"

type CellResponse = {
  activityId: number; activityName: string; color: string
  cells: Array<{ iso: string; secs: number; min: number; total: string }>
  rowTotal: number; rowTotalLabel: string
  dayTotals: Array<{ secs: number; total: string }>
  grandTotal: number; grandTotalLabel: string
  error?: string
}

export function TimesheetPage({ initial }: { initial: TimesheetData }) {
  const [data, setData] = React.useState(initial)
  const [drafts, setDrafts] = React.useState<Record<string, string>>({})
  const [saving, setSaving] = React.useState<string | null>(null)
  const [error, setError] = React.useState("")
  const [newActivity, setNewActivity] = React.useState("")
  const lang = data.Lang

  function cellKey(activityId: number, index: number) { return `${activityId}:${index}` }

  async function saveCell(row: TimesheetRow, cell: TimesheetRow["Cells"][number], value: string) {
    const key = cellKey(row.ActivityID, cell.Index)
    const minutes = value.trim() === "" ? 0 : Number(value)
    if (!Number.isInteger(minutes) || minutes < 0 || minutes > 1440) {
      setError(t(lang, "err.invalidInput"))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    if (minutes === cell.Min) {
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    setSaving(key); setError("")
    const body = new URLSearchParams({ csrf_token: data.CSRFToken, activity_id: String(row.ActivityID), date: cell.ISO, minutes: String(minutes) })
    try {
      const response = await fetch("/api/timesheet/cell", {
        method: "POST", credentials: "same-origin", headers: {
          Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": data.CSRFToken,
        }, body,
      })
      if (!response.ok) throw new Error((await response.text()).replace(/<[^>]*>/g, " ").trim() || response.statusText)
      const update = await response.json() as CellResponse
      if (update.error) throw new Error(update.error)
      setData(previous => ({
        ...previous,
        Rows: previous.Rows.map(existing => existing.ActivityID === update.activityId ? {
          ...existing, ActivityName: update.activityName, Color: update.color, RowTotal: update.rowTotal, RowTotalLabel: update.rowTotalLabel,
          Cells: existing.Cells.map((existingCell, index) => {
            const cell = update.cells[index]
            return { ...existingCell, ISO: cell.iso, Secs: cell.secs, Min: cell.min, Total: cell.total }
          }),
        } : existing),
        DayTotalLabels: update.dayTotals.map(total => total.total),
        Days: previous.Days.map((day, index) => ({ ...day, Secs: update.dayTotals[index].secs, Min: Math.floor(update.dayTotals[index].secs / 60), Total: update.dayTotals[index].total })),
        GrandTotal: update.grandTotal, GrandTotalLabel: update.grandTotalLabel,
      }))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } finally { setSaving(null) }
  }

  function addRow() {
    if (!newActivity) return
    const query = new URLSearchParams({ date: data.DateISO })
    for (const id of data.Added) query.append("add", String(id))
    query.append("add", newActivity)
    window.location.assign(`/timesheet?${query}`)
  }

  return <main className="mx-auto w-full max-w-6xl space-y-4">
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "ts.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "ts.blurb")}</p></div>
      <nav className="week-nav flex items-center gap-1" aria-label={t(lang, "ts.title")}>
        <Button asChild variant="ghost" size="icon" title={t(lang, "sch.prev")}><a href={`/timesheet?date=${data.PrevWeek}`} aria-label={t(lang, "sch.prev")}><ArrowLeft aria-hidden="true" /></a></Button>
        <span className="week-nav-label whitespace-nowrap px-2 font-mono text-sm">{data.WeekLabel}</span>
        <Button asChild variant="ghost" size="icon" title={t(lang, "sch.next")}><a href={`/timesheet?date=${data.NextWeek}`} aria-label={t(lang, "sch.next")}><ArrowRight aria-hidden="true" /></a></Button>
        <Button asChild variant="ghost" size="sm"><a href="/timesheet">{t(lang, "ts.thisWeek")}</a></Button>
      </nav>
    </header>

    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    {data.Rows.length ? <Card className="overflow-hidden"><CardContent className="overflow-x-auto p-0">
      <table className="week-grid w-full min-w-[48rem] border-collapse text-sm" aria-busy={saving !== null}>
        <colgroup><col className="week-name-col" /><col span={7} /><col className="week-total-col" /></colgroup>
        <thead><tr className="border-b">
          <th className="sticky left-0 z-10 bg-card px-3 py-3 text-left font-medium">{t(lang, "dash.activity")}</th>
          {data.Days.map(day => <th key={day.ISO} className={`px-1 py-2 text-center font-medium ${day.IsToday ? "bg-muted" : ""}`}><span className="block text-xs uppercase tracking-wider text-muted-foreground">{day.Label}</span><span className="font-mono text-base">{day.Date}</span></th>)}
          <th className="px-3 py-2 text-right font-medium">{t(lang, "stats.total")}</th>
        </tr></thead>
        <tbody id="ts-body">
          {data.Rows.map(row => <tr key={row.ActivityID} id={`ts-row-${row.ActivityID}`} className="border-b last:border-0">
            <th scope="row" className="sticky left-0 z-10 max-w-56 bg-card px-3 py-2 text-left font-medium"><span className="grid-name flex min-w-0 items-center gap-2" title={row.ActivityName}><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: row.Color }} /><span className="truncate">{row.ActivityName}</span></span></th>
            {row.Cells.map(cell => {
              const key = cellKey(row.ActivityID, cell.Index)
              return <td key={cell.ISO} className={`p-1 text-center ${cell.IsToday ? "bg-muted/70" : ""}`}>
                <Input type="number" min={0} max={1440} step={5} inputMode="numeric" className="h-8 w-16 px-1 text-center font-mono text-xs tabular-nums" name="minutes" value={drafts[key] ?? (cell.Secs ? String(cell.Min) : "")} placeholder="—" title={cell.ISO} aria-label={`${row.ActivityName} — ${cell.ISO}`} disabled={saving !== null} onChange={event => setDrafts(previous => ({ ...previous, [key]: event.target.value }))} onBlur={event => void saveCell(row, cell, event.target.value)} />
              </td>
            })}
            <td className="whitespace-normal px-3 py-2 text-right font-mono leading-tight tabular-nums">{row.RowTotalLabel}</td>
          </tr>)}
        </tbody>
        <tfoot><tr className="border-t-2 font-semibold">
          <th className="sticky left-0 z-10 bg-card px-3 py-2 text-left">{t(lang, "stats.total")}</th>
          {data.DayTotalLabels.map((label, index) => <td key={data.Days[index].ISO} className="whitespace-normal px-1 py-2 text-center font-mono leading-tight tabular-nums">{label}</td>)}
          <td className="whitespace-normal px-3 py-2 text-right font-mono leading-tight tabular-nums">{data.GrandTotalLabel}</td>
        </tr></tfoot>
      </table>
    </CardContent></Card> : <Card><CardContent className="flex flex-col items-center py-8 text-center">
      <p className="font-medium">{t(lang, "ts.empty")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "ts.emptyHint")}</p>
      <Button asChild size="sm" className="mt-3"><a href="/"><Plus aria-hidden="true" />{t(lang, "ts.emptyCta")}</a></Button>
    </CardContent></Card>}

    {!!data.Others.length && <div className="flex flex-wrap items-center gap-2">
      <Select value={newActivity} onValueChange={setNewActivity}><SelectTrigger id="ts-add" className="w-full sm:w-72" aria-label={t(lang, "ts.addRow")}><SelectValue placeholder={`${t(lang, "ts.addRow")}…`} /></SelectTrigger><SelectContent>{data.Others.map(activity => <SelectItem key={activity.ID} value={String(activity.ID)}>{activity.Name}</SelectItem>)}</SelectContent></Select>
      <Button type="button" size="sm" disabled={!newActivity} onClick={addRow}><Plus aria-hidden="true" />{t(lang, "ts.addRowBtn")}</Button>
      <span className="w-full text-xs text-muted-foreground">{t(lang, "ts.addRowHint")}</span>
    </div>}
    <p className="text-sm text-muted-foreground">{t(lang, "ts.hint")}</p>
  </main>
}
