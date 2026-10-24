import { StatusLine } from "@/components/status-line"
import { Flash } from "@/components/flash"
import { Table, TableHeader, TableBody, TableFooter, TableRow, TableHead, TableCell } from "@/components/ui/table"
import * as React from "react"
import { ArrowLeft, ArrowRight, Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"
import type { TimesheetData, TimesheetRow } from "@/dashboard/types"
import { TimeEditRequests } from "./time-edit-requests"
import { activityColor } from "@/lib/activity-color"
import { SegmentedControl } from "@/components/segmented"
import { responseMessage } from "@/lib/response-message"
import { ColorMark } from "@/components/color-mark"

type CellResponse = {
  activityId: number; activityName: string; color: string
  cells: Array<{ iso: string; secs: number; min: number; total: string; live?: number }>
  rowTotal: number; rowTotalLabel: string
  dayTotals: Array<{ secs: number; total: string }>
  grandTotal: number; grandTotalLabel: string
  error?: string
}

// One undoable edit: which cell it was and the minutes the input showed before
// the edit. Minutes are the sheet's own precision, so undo restores exactly
// the number the person saw — including 0 for an empty day.
// A cell takes minutes («90») or hours and minutes («1:30»): the grid is read
// in hours, and a number field refused the colon people type for them.
// A cell reads like the totals beside it: «1:30», not «90». The input still
// takes either form; this is how a saved value is shown back.
export function cellClock(minutes: number): string {
  return `${Math.floor(minutes / 60)}:${String(minutes % 60).padStart(2, "0")}`
}

// Totals inside the grid use the cells' own form, so a column adds up at a
// glance and «2 ч 15 мин» no longer wraps in a 80px day column. The spelled
// out label stays in the tooltip.
function totalClock(secs: number): string {
  if (secs <= 0) return "—"
  if (secs < 60) return "<0:01"
  return cellClock(Math.floor(secs / 60))
}

// ↑ and ↓ move to the same day in the row above or below, as in a
// spreadsheet; Enter saves and moves down. Tab still walks along the row.
function moveFrom(input: HTMLInputElement, step: number) {
  const [row, day] = (input.dataset.tsCell || "").split(":").map(Number)
  const next = input.closest("[data-ts-grid]")?.querySelector<HTMLInputElement>(`input[data-ts-cell="${row + step}:${day}"]`)
  if (!next) return
  next.focus()
  next.select()
}

// What Enter may move on from: an unreadable value keeps the focus where
// the cell is marked.
const readable = (value: string) => { const minutes = cellMinutes(value); return Number.isInteger(minutes) && minutes >= 0 && minutes <= 1440 }

export function cellMinutes(value: string): number {
  const text = value.trim()
  if (text === "") return 0
  const clock = text.match(/^(\d{1,2}):(\d{2})$/)
  if (clock) return Number(clock[2]) < 60 ? Number(clock[1]) * 60 + Number(clock[2]) : NaN
  return /^\d+$/.test(text) ? Number(text) : NaN
}

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
  // The cell that could not be read: marked where it is, not in a banner
  // above the table that pushed the grid down and named no cell.
  const [invalid, setInvalid] = React.useState<string | null>(null)
  const [newActivity, setNewActivity] = React.useState("")
  const lang = data.Lang
  // A week a manager approved is read-only for everyone until it is reopened.
  const approved = data.Approval.Approved
  // A screen reader read «2026-10-05» digit by digit; the grid's own header says «пн 5».
  // Under a minute a cell shows «<1» like its row total, not a rounded-up «1».
  const dayName = (iso: string) => { const day = data.Days.find(item => item.ISO === iso); return day ? `${day.Label} ${day.Date}` : iso }
  const busy = saving !== null || clearing
  const lastEdit = history[history.length - 1] ?? null
  // A row whose activity has no project still takes minutes, but its time
  // reaches no project report — say so once instead of leaving "Без проекта"
  // to be read as "the workspace has no projects".
  const unlinkedRows = data.Rows.filter(row => !data.ProjectNames?.[row.ProjectID])
  const projectOf = (row: TimesheetRow) => data.ProjectNames?.[row.ProjectID] || t(lang, "dash.uncategorized")
  // The project under the activity is the way into that project's own screen,
  // so the sheet is not a dead end: booked hours lead back to where they went.
  // A phone opens on today: the week is seven days of scrolling for someone who
  // almost always means "what did I do today". The week is one tap away.
  const [onlyToday, setOnlyToday] = React.useState(true)
  const daysShown = onlyToday ? data.Days.filter(day => day.IsToday) : data.Days
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
          return { ...existingCell, ISO: cell.iso, Secs: cell.secs, Min: cell.min, Total: cell.total, Live: cell.live ?? 0 }
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
      if (!response.ok) throw new Error(await responseMessage(response))
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

  // The last value tried for a cell. Leaving the cell does not retry a write
  // that just failed (it raced the undo the person clicked); Enter does.
  const attempted = React.useRef<{ key: string; value: string } | null>(null)

  async function saveCell(row: TimesheetRow, cell: TimesheetRow["Cells"][number], value: string, retry = false) {
    const key = cellKey(row.ActivityID, cell.Index)
    if (!retry && attempted.current?.key === key && attempted.current.value === value) return
    // Tabbing through a cell is not an edit: a «<1» cell read as 0 and wrote
    // minutes=0 for a day nobody touched.
    if (!(key in drafts) || saving === key) return
    const minutes = cellMinutes(value)
    if (!Number.isInteger(minutes) || minutes < 0 || minutes > 1440) {
      setInvalid(key)
      return
    }
    setInvalid(null)
    if (minutes === cell.Min) {
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    attempted.current = { key, value }
    const before: CellEdit = { activityId: row.ActivityID, activity: row.ActivityName, date: cell.ISO, index: cell.Index, minutes: cell.Min }
    setNotice(null)
    if (!await writeCell(row, cell, minutes)) return
    attempted.current = null
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
      if (!response.ok) throw new Error(await responseMessage(response))
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
    : t(lang, notice.kind === "restored" ? "ts.undone" : "ts.saved", notice.activity, notice.date ? dayName(notice.date) : "")

  return <main className="mx-auto w-full max-w-6xl space-y-4">
    <Flash message={initial.Flash} ok={initial.FlashOK} />
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "ts.title")}</h1></div>
      <nav className="week-nav flex items-center gap-1" aria-label={t(lang, "ts.title")}>
        <Button asChild variant="ghost" size="icon" title={t(lang, "sch.prev")}><a href={`/timesheet?date=${data.PrevWeek}`} aria-label={t(lang, "sch.prev")}><ArrowLeft aria-hidden="true" /></a></Button>
        <span className="week-nav-label whitespace-nowrap px-2 font-mono text-sm">{data.WeekLabel}</span>
        <Button asChild variant="ghost" size="icon" title={t(lang, "sch.next")}><a href={`/timesheet?date=${data.NextWeek}`} aria-label={t(lang, "sch.next")}><ArrowRight aria-hidden="true" /></a></Button>
        <Button asChild variant="ghost" size="sm"><a href="/timesheet">{t(lang, "ts.thisWeek")}</a></Button>
      </nav>
    </header>

    <p className="text-sm text-muted-foreground">{t(lang, "ts.saveHint")}</p>
    {data.AutoLockNotice && <p className="text-sm text-muted-foreground">{data.AutoLockNotice}</p>}
    <div role="status" className="flex flex-wrap items-center gap-2 rounded-md border p-3 text-sm">
      <span className="font-medium">{approved ? t(lang, "ts.approvedOn", data.Approval.ApprovedOn) : t(lang, "ts.notApproved")}</span>
      {approved && <span className="text-muted-foreground">{t(lang, data.Approval.CanManage ? "ts.lockedManager" : "ts.lockedMember")}</span>}
      {!approved && !data.Approval.Finished && <span className="text-muted-foreground">{t(lang, "ts.weekRunningNote")}</span>}
      {data.Approval.CanManage && approved && (
        <form method="post" action="/api/timesheet/reopen">
          <input type="hidden" name="csrf_token" value={data.CSRFToken} />
          <input type="hidden" name="start" value={data.Approval.StartISO} />
          <input type="hidden" name="end" value={data.Approval.EndISO} />
          <Button type="submit" variant="outline" size="sm">{t(lang, "ts.reopen")}</Button>
        </form>
      )}
      {data.Approval.CanManage && !approved && data.Approval.Finished && (
        <form method="post" action="/api/timesheet/approve" data-confirm={t(lang, "ts.approveConfirm")}>
          <input type="hidden" name="csrf_token" value={data.CSRFToken} />
          <input type="hidden" name="start" value={data.Approval.StartISO} />
          <input type="hidden" name="end" value={data.Approval.EndISO} />
          <Button type="submit" size="sm">{t(lang, "ts.approve")}</Button>
        </form>
      )}
    </div>
    <div className="flex flex-wrap items-center gap-2">
      <StatusLine text={saving ? t(lang, "ts.saving") : noticeText} className="text-sm" />
      {lastEdit && <Button type="button" variant="ghost" size="sm" disabled={busy} title={t(lang, "ts.undoCell", lastEdit.activity, lastEdit.date)} onClick={() => void undoLastEdit()}>{t(lang, "toast.undo")}</Button>}
    </div>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    {data.Rows.length ? <>
      {/* A week is seven days wide. On a phone that meant a sideways scroll on
          every row and two of seven days in view, so below the small
          breakpoint the sheet becomes one block per day — same inputs, same
          save path, nothing typed twice. */}
      <SegmentedControl
        label={t(lang, "ts.thisWeek")}
        value={onlyToday ? "today" : "week"}
        className="ts-day-switch"
        options={[
          { key: "today", label: t(lang, "ts.todayOnly"), onSelect: () => setOnlyToday(true) },
          { key: "week", label: t(lang, "ts.wholeWeek"), onSelect: () => setOnlyToday(false) },
        ]}
      />
      <div className="ts-day-list grid gap-3" data-ts-grid>
        {daysShown.map(day => {
          const dayIndex = data.Days.indexOf(day)
          const label = `${day.Label} ${day.Date}`
          return <Card key={day.ISO} className={day.IsToday ? "min-w-0 border-foreground/20" : "min-w-0"}>
            <CardHeader className="flex-row items-center justify-between gap-2 px-3 py-2">
              <CardTitle className="text-sm">{label}</CardTitle>
              <span className="font-mono text-sm tabular-nums text-muted-foreground">{data.DayTotalLabels[dayIndex]}</span>
            </CardHeader>
            <CardContent className="grid gap-2 px-3 pb-3">
              {data.Rows.map((row, rowIndex) => {
                const cell = row.Cells[dayIndex]
                const key = cellKey(row.ActivityID, cell.Index)
                return <div key={row.ActivityID} className="flex min-w-0 items-start gap-2">
                  <span className="grid-name flex min-w-0 flex-1 items-start gap-2">
                    <ColorMark color={activityColor(row.Color)} />
                    <span className="min-w-0"><span className="block break-words text-sm font-medium [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:3] overflow-hidden" title={row.ActivityName}>{row.ActivityName}</span>
                      <span className="block truncate text-xs font-normal text-muted-foreground" title={projectOf(row)}>{projectOf(row)}</span></span>
                  </span>
                  <span className="grid shrink-0 justify-items-center gap-0.5"><Input type="text" inputMode="text" autoComplete="off" spellCheck={false} className="w-20 shrink-0 px-2 text-center font-mono tabular-nums" name="minutes" data-ts-cell={`${rowIndex}:${cell.Index}`} value={drafts[key] ?? (cell.Min > 0 && cell.Secs - (cell.Live ?? 0) >= 60 ? cellClock(cell.Min) : "")} placeholder={cell.Secs - (cell.Live ?? 0) > 0 && cell.Secs - (cell.Live ?? 0) < 60 ? "<1" : "—"} title={dayName(cell.ISO)} aria-label={`${row.ActivityName} — ${projectOf(row)} — ${dayName(cell.ISO)}`} readOnly={approved || saving === key || clearing} aria-busy={saving === key || undefined} onChange={event => { setDrafts(previous => ({ ...previous, [key]: event.target.value })); if (invalid === key) setInvalid(null) }} aria-invalid={invalid === key || undefined} onKeyDown={event => {
                  if (event.key === "Enter") { event.preventDefault(); const typed = event.currentTarget.value; void saveCell(row, cell, typed, true); if (readable(typed)) moveFrom(event.currentTarget, 1) }
                  else if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); moveFrom(event.currentTarget, event.key === "ArrowDown" ? 1 : -1) } }} onBlur={event => void saveCell(row, cell, event.target.value)} />{invalid === key ? <span role="alert" className="block max-w-24 text-center text-xs leading-4 text-destructive">{t(lang, "ts.badCell")}</span> : null}{cell.Live ? <span data-cell-live className="block text-center text-xs leading-4 text-muted-foreground" title={t(lang, "ts.liveHint")}>+{cell.Live < 60 ? "<1" : Math.floor(cell.Live / 60)}</span> : <span aria-hidden="true" className="block h-4" />}</span>
                  <Button type="button" variant="ghost" size="icon" className="ts-row-clear shrink-0" disabled={busy || approved} title={t(lang, "ts.clearRow", row.ActivityName)} aria-label={t(lang, "ts.clearRow", row.ActivityName)} onClick={() => void clearRow(row)}><Trash2 aria-hidden="true" /></Button>
                </div>
              })}
            </CardContent>
          </Card>
        })}
        <p className="text-sm font-semibold"><span className="text-muted-foreground">{t(lang, "stats.total")}: </span><span className="font-mono tabular-nums">{data.GrandTotalLabel}</span></p>
      </div>

      <Card className="ts-week-grid-card overflow-hidden"><CardContent className="overflow-x-auto p-0">
      <Table className="week-grid w-full min-w-[48rem] border-collapse text-sm" aria-busy={busy}>
        <colgroup><col className="week-name-col" /><col span={7} /><col className="week-total-col" /><col className="w-10" /></colgroup>
        <TableHeader><TableRow className="border-b">
          <TableHead className="sticky left-0 z-10 bg-card px-3 py-3 text-left font-medium">{t(lang, "dash.activity")}</TableHead>
          {data.Days.map(day => <TableHead key={day.ISO} className={`px-1 py-2 text-center font-medium ${day.IsToday ? "bg-muted" : ""}`}><span className="block text-xs uppercase tracking-wider text-muted-foreground">{day.Label}</span><span className="font-mono text-base">{day.Date}</span></TableHead>)}
          <TableHead className="px-3 py-2 text-right font-medium">{t(lang, "stats.total")}</TableHead>
          <TableHead className="px-1 py-2 text-right font-medium"><span className="sr-only">{t(lang, "ts.rowActions")}</span></TableHead>
        </TableRow></TableHeader>
        <TableBody id="ts-body" data-ts-grid>
          {data.Rows.map((row, rowIndex) => <TableRow key={row.ActivityID} id={`ts-row-${row.ActivityID}`} className="border-b last:border-0">
            <TableHead scope="row" className="sticky left-0 z-10 max-w-56 bg-card px-3 py-2 text-left font-medium"><span className="grid-name flex min-w-0 items-start gap-2" title={row.ActivityName}><ColorMark color={activityColor(row.Color)} /><span className="min-w-0"><span className="block whitespace-normal [overflow-wrap:anywhere]">{row.ActivityName}</span><span className="block whitespace-normal text-xs font-normal text-muted-foreground [overflow-wrap:anywhere]">{projectLabel(row)}</span></span></span></TableHead>
            {row.Cells.map(cell => {
              const key = cellKey(row.ActivityID, cell.Index)
              return <TableCell key={cell.ISO} className={`p-1 text-center align-top ${cell.IsToday ? "bg-muted/70" : ""}`}>
                <Input type="text" inputMode="text" autoComplete="off" spellCheck={false} className="h-8 w-16 px-1 text-center font-mono text-xs tabular-nums" name="minutes" data-ts-cell={`${rowIndex}:${cell.Index}`} value={drafts[key] ?? (cell.Min > 0 && cell.Secs - (cell.Live ?? 0) >= 60 ? cellClock(cell.Min) : "")} placeholder={cell.Secs - (cell.Live ?? 0) > 0 && cell.Secs - (cell.Live ?? 0) < 60 ? "<1" : "—"} title={dayName(cell.ISO)} aria-label={`${row.ActivityName} — ${projectOf(row)} — ${dayName(cell.ISO)}`} readOnly={approved || saving === key || clearing} aria-busy={saving === key || undefined} onChange={event => { setDrafts(previous => ({ ...previous, [key]: event.target.value })); if (invalid === key) setInvalid(null) }} aria-invalid={invalid === key || undefined} onKeyDown={event => {
                  if (event.key === "Enter") { event.preventDefault(); const typed = event.currentTarget.value; void saveCell(row, cell, typed, true); if (readable(typed)) moveFrom(event.currentTarget, 1) }
                  else if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); moveFrom(event.currentTarget, event.key === "ArrowDown" ? 1 : -1) } }} onBlur={event => void saveCell(row, cell, event.target.value)} />{invalid === key ? <span role="alert" className="block max-w-24 text-center text-xs leading-4 text-destructive">{t(lang, "ts.badCell")}</span> : null}{cell.Live ? <span data-cell-live className="block text-center text-xs leading-4 text-muted-foreground" title={t(lang, "ts.liveHint")}>+{cell.Live < 60 ? "<1" : Math.floor(cell.Live / 60)}</span> : <span aria-hidden="true" className="block h-4" />}
              </TableCell>
            })}
            <TableCell className="whitespace-nowrap px-3 py-2 text-right font-mono leading-tight tabular-nums" title={row.RowTotalLabel}>{totalClock(row.RowTotal)}</TableCell>
            <TableCell className="px-1 py-1 text-right align-top"><Button type="button" variant="ghost" size="icon-sm" disabled={busy || approved} title={t(lang, "ts.clearRow", row.ActivityName)} aria-label={t(lang, "ts.clearRow", row.ActivityName)} onClick={() => void clearRow(row)}><Trash2 aria-hidden="true" /></Button></TableCell>
          </TableRow>)}
        </TableBody>
        <TableFooter><TableRow className="border-t font-semibold">
          <TableHead className="sticky left-0 z-10 bg-card px-3 py-2 text-left">{t(lang, "stats.total")}</TableHead>
          {data.DayTotalLabels.map((label, index) => <TableCell key={data.Days[index].ISO} className="whitespace-nowrap px-1 py-2 text-center font-mono leading-tight tabular-nums" title={label}>{totalClock(data.Days[index].Secs)}</TableCell>)}
          <TableCell className="whitespace-nowrap px-3 py-2 text-right font-mono font-semibold leading-tight tabular-nums" title={data.GrandTotalLabel}>{totalClock(data.GrandTotal)}</TableCell>
        </TableRow></TableFooter>
      </Table>
    </CardContent></Card>
    </> : <Card><CardContent className="flex flex-col items-center py-8 text-center">
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
    <TimeEditRequests lang={lang} csrf={data.CSRFToken} days={data.Days} rows={data.Rows} requests={data.EditRequests} canDecide={data.CanDecide} />
  </main>
}
