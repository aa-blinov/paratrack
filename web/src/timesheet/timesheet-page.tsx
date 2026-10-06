import { Table, TableHeader, TableBody, TableFooter, TableRow, TableHead, TableCell } from "@/components/ui/table"
import * as React from "react"
import { ArrowLeft, ArrowRight, Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { requestConfirmation } from "@/components/confirmation-dialog"
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

// One undoable edit: which cell it was and the minutes the input showed before
// the edit. Minutes are the sheet's own precision, so undo restores exactly
// the number the person saw — including 0 for an empty day.
type CellEdit = { activityId: number; activity: string; date: string; index: number; minutes: number }

// undoDepth is how many recent edits stay undoable from the status line. One
// entry covers "typed the wrong number"; ten covers a week filled in cell by
// cell, and the oldest entry is dropped instead of kept as a stale value that
// would silently resurrect old hours.
const undoDepth = 10

type Notice = { kind: "saved" | "restored" | "cleared"; activity: string; date?: string }

export function TimesheetPage({ initial }: { initial: TimesheetData }) {
  const [data, setData] = React.useState(initial)
  const [drafts, setDrafts] = React.useState<Record<string, string>>({})
  const [saving, setSaving] = React.useState<string | null>(null)
  const [clearing, setClearing] = React.useState(false)
  const [notice, setNotice] = React.useState<Notice | null>(null)
  const [history, setHistory] = React.useState<CellEdit[]>([])
  const [error, setError] = React.useState("")
  const [newActivity, setNewActivity] = React.useState("")
  const lang = data.Lang
  const busy = saving !== null || clearing
  const lastEdit = history[history.length - 1] ?? null
  // A row whose activity has no project still takes minutes, but its time
  // reaches no project report — say so once instead of leaving "Без проекта"
  // to be read as "the workspace has no projects".
  const unlinkedRows = data.Rows.filter(row => !data.ProjectNames?.[row.ProjectID])
  const projectOf = (row: TimesheetRow) => data.ProjectNames?.[row.ProjectID] || t(lang, "dash.uncategorized")
  // The project under the activity is the way into that project's own screen,
  // so the sheet is not a dead end: booked hours lead back to where they went.
  const projectLabel = (row: TimesheetRow) => {
    const slug = data.ProjectSlugs?.[row.ProjectID]
    const name = data.ProjectNames?.[row.ProjectID]
    if (!name) return t(lang, "dash.uncategorized")
    if (!slug) return name
    return <a href={`/projects/${encodeURIComponent(slug)}`} title={t(lang, "ts.openProject")} className="underline-offset-4 hover:underline">{name}</a>
  }

  function cellKey(activityId: number, index: number) { return `${activityId}:${index}` }

  // applyRow folds the row, day totals and week total the server recalculated
  // into the page, so one response keeps the whole grid consistent.
  function applyRow(update: CellResponse) {
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
  }

  function forgetDrafts(activityId: number) {
    const prefix = `${activityId}:`
    setDrafts(previous => { const next = { ...previous }; for (const key of Object.keys(next)) if (key.startsWith(prefix)) delete next[key]; return next })
  }

  // writeCell sends one cell value and reports whether the server accepted it.
  // Callers decide what the notice says, so undo can name the cell it undid.
  async function writeCell(row: TimesheetRow, cell: TimesheetRow["Cells"][number], minutes: number) {
    const key = cellKey(row.ActivityID, cell.Index)
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
      applyRow(update)
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      return false
    } finally { setSaving(null) }
  }

  async function saveCell(row: TimesheetRow, cell: TimesheetRow["Cells"][number], value: string) {
    const key = cellKey(row.ActivityID, cell.Index)
    const minutes = value.trim() === "" ? 0 : Number(value)
    if (!Number.isInteger(minutes) || minutes < 0 || minutes > 1440) {
      setNotice(null)
      setError(t(lang, "err.invalidInput"))
      return
    }
    if (minutes === cell.Min) {
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    const before: CellEdit = { activityId: row.ActivityID, activity: row.ActivityName, date: cell.ISO, index: cell.Index, minutes: cell.Min }
    setNotice(null)
    if (!await writeCell(row, cell, minutes)) return
    setNotice({ kind: "saved", activity: before.activity, date: before.date })
    setHistory(previous => [...previous, before].slice(-undoDepth))
  }

  // Undo always rewrites the server, even when the restored minutes match what
  // the response already shows: the cell was emptied with 0 before, so the
  // sheet has to be put back rather than left as the person last left it.
  // The entry stays on a failed write so the button can be pressed again.
  async function undoLastEdit() {
    const entry = lastEdit
    if (!entry) return
    const row = data.Rows.find(item => item.ActivityID === entry.activityId)
    const cell = row?.Cells[entry.index]
    if (!row || !cell) {
      setHistory(previous => previous.slice(0, -1))
      setNotice(null)
      setError(t(lang, "ts.undoMissing"))
      return
    }
    setNotice(null)
    if (!await writeCell(row, cell, entry.minutes)) return
    setHistory(previous => previous.filter(item => item !== entry))
    setNotice({ kind: "restored", activity: entry.activity, date: entry.date })
  }

  // clearRow empties a whole activity row for the week on screen. It is
  // destructive, so it asks first and reports the empty row it produced.
  async function clearRow(row: TimesheetRow) {
    if (!await requestConfirmation(t(lang, "ts.clearRowConfirm", row.ActivityName))) return
    setClearing(true); setError(""); setNotice(null)
    const body = new URLSearchParams({ csrf_token: data.CSRFToken, activity_id: String(row.ActivityID), date: data.DateISO })
    try {
      const response = await fetch("/api/timesheet/row/clear", {
        method: "POST", credentials: "same-origin", headers: {
          Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": data.CSRFToken,
        }, body,
      })
      if (!response.ok) throw new Error((await response.text()).replace(/<[^>]*>/g, " ").trim() || response.statusText)
      const update = await response.json() as CellResponse
      if (update.error) throw new Error(update.error)
      applyRow(update)
      forgetDrafts(row.ActivityID)
      // A row that is empty again has nothing left to undo in it.
      setHistory(previous => previous.filter(item => item.activityId !== row.ActivityID))
      setNotice({ kind: "cleared", activity: row.ActivityName })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally { setClearing(false) }
  }

  function addRow() {
    if (!newActivity) return
    const query = new URLSearchParams({ date: data.DateISO })
    for (const id of data.Added) query.append("add", String(id))
    query.append("add", newActivity)
    window.location.assign(`/timesheet?${query}`)
  }

  const noticeText = !notice ? "" : notice.kind === "cleared"
    ? t(lang, "ts.rowCleared", notice.activity)
    : t(lang, notice.kind === "restored" ? "ts.undone" : "ts.saved", notice.activity, notice.date || "")

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

    <p className="text-sm text-muted-foreground">{t(lang, "ts.saveHint")}</p>
    <div className="flex flex-wrap items-center gap-2">
      <p role="status" aria-live="polite" className="min-h-5 text-sm">{saving ? t(lang, "ts.saving") : noticeText}</p>
      {lastEdit && <Button type="button" variant="ghost" size="sm" disabled={busy} title={t(lang, "ts.undoCell", lastEdit.activity, lastEdit.date)} onClick={() => void undoLastEdit()}>{t(lang, "toast.undo")}</Button>}
    </div>
    <p className="text-xs text-muted-foreground sm:hidden">{t(lang, "ts.scrollHint")}</p>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    {data.Rows.length ? <Card className="overflow-hidden"><CardContent className="overflow-x-auto p-0">
      <Table className="week-grid w-full min-w-[48rem] border-collapse text-sm" aria-busy={busy}>
        <colgroup><col className="week-name-col" /><col span={7} /><col className="week-total-col" /><col className="w-10" /></colgroup>
        <TableHeader><TableRow className="border-b">
          <TableHead className="sticky left-0 z-10 bg-card px-3 py-3 text-left font-medium">{t(lang, "dash.activity")}</TableHead>
          {data.Days.map(day => <TableHead key={day.ISO} className={`px-1 py-2 text-center font-medium ${day.IsToday ? "bg-muted" : ""}`}><span className="block text-xs uppercase tracking-wider text-muted-foreground">{day.Label}</span><span className="font-mono text-base">{day.Date}</span></TableHead>)}
          <TableHead className="px-3 py-2 text-right font-medium">{t(lang, "stats.total")}</TableHead>
          <TableHead className="px-1 py-2 text-right font-medium"><span className="sr-only">{t(lang, "ts.rowActions")}</span></TableHead>
        </TableRow></TableHeader>
        <TableBody id="ts-body">
          {data.Rows.map(row => <TableRow key={row.ActivityID} id={`ts-row-${row.ActivityID}`} className="border-b last:border-0">
            <TableHead scope="row" className="sticky left-0 z-10 max-w-56 bg-card px-3 py-2 text-left font-medium"><span className="grid-name flex min-w-0 items-center gap-2" title={row.ActivityName}><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: row.Color }} /><span className="min-w-0"><span className="block whitespace-normal [overflow-wrap:anywhere]">{row.ActivityName}</span><span className="block whitespace-normal text-xs font-normal text-muted-foreground [overflow-wrap:anywhere]">{projectLabel(row)}</span></span></span></TableHead>
            {row.Cells.map(cell => {
              const key = cellKey(row.ActivityID, cell.Index)
              return <TableCell key={cell.ISO} className={`p-1 text-center ${cell.IsToday ? "bg-muted/70" : ""}`}>
                <Input type="number" min={0} max={1440} step={5} inputMode="numeric" className="h-8 w-16 px-1 text-center font-mono text-xs tabular-nums" name="minutes" value={drafts[key] ?? (cell.Secs ? String(cell.Min) : "")} placeholder="—" title={cell.ISO} aria-label={`${row.ActivityName} — ${projectOf(row)} — ${cell.ISO}`} disabled={busy} onChange={event => setDrafts(previous => ({ ...previous, [key]: event.target.value }))} onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); event.currentTarget.blur() } }} onBlur={event => void saveCell(row, cell, event.target.value)} />
              </TableCell>
            })}
            <TableCell className="whitespace-normal px-3 py-2 text-right font-mono leading-tight tabular-nums">{row.RowTotalLabel}</TableCell>
            <TableCell className="px-1 py-2 text-right"><Button type="button" variant="ghost" size="icon" disabled={busy} title={t(lang, "ts.clearRow", row.ActivityName)} aria-label={t(lang, "ts.clearRow", row.ActivityName)} onClick={() => void clearRow(row)}><Trash2 aria-hidden="true" /></Button></TableCell>
          </TableRow>)}
        </TableBody>
        <TableFooter><TableRow className="border-t-2 font-semibold">
          <TableHead className="sticky left-0 z-10 bg-card px-3 py-2 text-left">{t(lang, "stats.total")}</TableHead>
          {data.DayTotalLabels.map((label, index) => <TableCell key={data.Days[index].ISO} className="whitespace-normal px-1 py-2 text-center font-mono leading-tight tabular-nums">{label}</TableCell>)}
          <TableCell className="whitespace-normal whitespace-normal px-3 py-2 text-right font-mono leading-tight tabular-nums">{data.GrandTotalLabel}</TableCell>
        </TableRow></TableFooter>
      </Table>
    </CardContent></Card> : <Card><CardContent className="flex flex-col items-center py-8 text-center">
      <p className="font-medium">{t(lang, "ts.empty")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "ts.emptyHint")}</p>
      <Button asChild size="sm" className="mt-3"><a href="/"><Plus aria-hidden="true" />{t(lang, "ts.emptyCta")}</a></Button>
    </CardContent></Card>}

    {!!unlinkedRows.length && <p data-no-project-hint className="text-sm text-muted-foreground">{t(lang, "ts.noProjectHint")}</p>}
    {!!data.Others.length && <div className="flex flex-wrap items-center gap-2">
      <Select value={newActivity} onValueChange={setNewActivity}><SelectTrigger id="ts-add" className="w-full sm:w-72" aria-label={t(lang, "ts.addRow")}><SelectValue placeholder={`${t(lang, "ts.addRow")}…`} /></SelectTrigger><SelectContent>{data.Others.map(activity => <SelectItem key={activity.ID} value={String(activity.ID)}>{activity.Name} — {data.ProjectNames?.[activity.ProjectID] || t(lang, "dash.uncategorized")}</SelectItem>)}</SelectContent></Select>
      <Button type="button" size="sm" disabled={!newActivity} onClick={addRow}><Plus aria-hidden="true" />{t(lang, "ts.addRowBtn")}</Button>
      <span className="w-full text-xs text-muted-foreground">{t(lang, "ts.addRowHint")}</span>
    </div>}
    <p className="text-sm text-muted-foreground">{t(lang, "ts.hint")}</p>
  </main>
}
