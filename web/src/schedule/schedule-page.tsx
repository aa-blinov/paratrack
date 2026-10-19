import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import * as React from "react"
import { ArrowLeft, ArrowRight, Copy, Eye, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { ScheduleData, ScheduleDay, SchedulePerson, ScheduleLine } from "@/dashboard/types"

// The plan as the server sends it after any change: the week, again.
type PlanUpdate = {
  days: ScheduleDay[]
  people: SchedulePerson[]
  grandTotal: string
  grandMin: number
  weekEmpty: boolean
  copyFrom: boolean
  error?: string
}

type PendingLine = { ProjectID: number; ProjectName: string; Color: string }

const DAY_MAX = 24 * 60
const ADMIN_MAX = 7 * 24 * 60

const pad2 = (n: number) => String(n).padStart(2, "0")

// parseMinutes reads what a person types into an hours field: "4", "4,5",
// "4.5", "4:30". Empty is zero. Anything else, or more than max, is null.
export function parseMinutes(text: string, max: number): number | null {
  const value = text.trim()
  if (value === "") return 0
  let minutes: number
  const clock = /^(\d{1,3}):([0-5]\d)$/.exec(value)
  if (clock) {
    minutes = Number(clock[1]) * 60 + Number(clock[2])
  } else {
    const decimal = /^(\d{1,3})(?:[.,](\d{1,2}))?$/.exec(value)
    if (!decimal) return null
    const fraction = decimal[2] ? Number(decimal[2]) / 10 ** decimal[2].length : 0
    minutes = Math.round((Number(decimal[1]) + fraction) * 60)
  }
  return minutes <= max ? minutes : null
}

// inputValue shows minutes in the field: h:mm for the clock format, hours
// with a comma otherwise, so what is shown can be typed back.
export function inputValue(minutes: number, format: string): string {
  if (minutes <= 0) return ""
  if (format === "clock") return `${Math.floor(minutes / 60)}:${pad2(minutes % 60)}`
  const hours = Math.round((minutes / 60) * 100) / 100
  return String(hours).replace(".", ",")
}

// durationLabel mirrors the server's duration labels, so a total typed on
// the page reads the same as the total the server writes back.
export function durationLabel(minutes: number, format: string, lang: string): string {
  const ru = lang === "ru"
  if (format === "clock") return `${Math.floor(Math.max(minutes, 0) / 60)}:${pad2(Math.max(minutes, 0) % 60)}`
  if (format === "decimal") {
    const hundredths = Math.round((Math.max(minutes, 0) / 60) * 100)
    const text = `${Math.floor(hundredths / 100)}${ru ? "," : "."}${pad2(hundredths % 100)}`
    return text + (ru ? " ч" : " h")
  }
  const hu = ru ? " ч" : "h"
  const mu = ru ? " мин" : "m"
  if (minutes <= 0) return "0" + mu
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  if (h > 0 && m > 0) return `${h}${hu} ${m}${mu}`
  if (h > 0) return `${h}${hu}`
  return `${m}${mu}`
}

function cellKey(userId: number, projectId: number, index: number) {
  return `${userId}:${projectId}:${index}`
}

export function SchedulePage({ initial }: { initial: ScheduleData }) {
  const [data, setData] = React.useState(initial)
  const [drafts, setDrafts] = React.useState<Record<string, string>>({})
  const [adminDrafts, setAdminDrafts] = React.useState<Record<number, string>>({})
  const [pending, setPending] = React.useState<Record<number, PendingLine[]>>({})
  const [saving, setSaving] = React.useState<string | null>(null)
  const [error, setError] = React.useState("")
  const lang = data.Lang
  const format = data.DurationFormat
  const canEdit = data.CanManage

  function applyPlan(next: PlanUpdate) {
    setData(previous => ({
      ...previous,
      Days: next.days,
      People: next.people,
      GrandTotal: next.grandTotal,
      GrandMin: next.grandMin,
      WeekEmpty: next.weekEmpty,
      CopyFrom: next.copyFrom,
    }))
  }

  async function send(url: string, fields: Record<string, string>): Promise<PlanUpdate> {
    const response = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        "X-CSRF-Token": data.CSRFToken,
      },
      body: new URLSearchParams({ csrf_token: data.CSRFToken, ...fields }),
    })
    const update = (await response.json()) as PlanUpdate
    if (!response.ok) throw new Error(update.error || response.statusText)
    return update
  }

  function fail(cause: unknown) {
    setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
  }

  // The minutes a cell shows right now: what is being typed, else saved.
  function cellMinutes(person: SchedulePerson, line: ScheduleLine, index: number): number {
    const draft = drafts[cellKey(person.UserID, line.ProjectID, index)]
    if (draft !== undefined) {
      const parsed = parseMinutes(draft, DAY_MAX)
      if (parsed !== null) return parsed
    }
    return line.Cells[index]?.Min ?? 0
  }

  async function saveCell(person: SchedulePerson, line: ScheduleLine, index: number, value: string) {
    const key = cellKey(person.UserID, line.ProjectID, index)
    const minutes = parseMinutes(value, DAY_MAX)
    if (minutes === null) {
      setError(t(lang, "err.invalidInput"))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    if (minutes === (line.Cells[index]?.Min ?? 0)) {
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
      return
    }
    setSaving(key)
    setError("")
    try {
      const update = await send("/api/schedule/cell", {
        user_id: String(person.UserID),
        project_id: String(line.ProjectID),
        date: data.Days[index].ISO,
        minutes: String(minutes),
      })
      applyPlan(update)
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } catch (cause) {
      fail(cause)
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } finally {
      setSaving(null)
    }
  }

  async function saveAdmin(person: SchedulePerson, value: string) {
    const minutes = parseMinutes(value, ADMIN_MAX)
    setAdminDrafts(previous => { const next = { ...previous }; delete next[person.UserID]; return next })
    if (minutes === null) {
      setError(t(lang, "err.invalidInput"))
      return
    }
    if (minutes === person.AdminMin) return
    setSaving(`admin:${person.UserID}`)
    setError("")
    try {
      applyPlan(await send("/api/schedule/admin", {
        user_id: String(person.UserID),
        minutes: String(minutes),
        week: data.ThisWeek,
      }))
    } catch (cause) {
      fail(cause)
    } finally {
      setSaving(null)
    }
  }

  async function copyWeek() {
    setSaving("copy")
    setError("")
    try {
      applyPlan(await send("/api/schedule/copy", { week: data.ThisWeek }))
    } catch (cause) {
      fail(cause)
    } finally {
      setSaving(null)
    }
  }

  // Live numbers: a person's week and each day, counted from what is typed.
  function liveTotals(person: SchedulePerson, lines: ScheduleLine[]) {
    const dayMins = data.Days.map((_, index) => lines.reduce((sum, line) => sum + cellMinutes(person, line, index), 0))
    const planned = dayMins.reduce((sum, minutes) => sum + minutes, 0)
    const available = person.AvailableMin
    const over = available > 0 ? planned > available : planned > 0
    const pct = available > 0 ? Math.floor((planned * 100) / available) : 0
    return { dayMins, planned, pct, over }
  }

  function linesOf(person: SchedulePerson): ScheduleLine[] {
    const known = new Set((person.Lines ?? []).map(line => line.ProjectID))
    const extra: ScheduleLine[] = (pending[person.UserID] ?? [])
      .filter(line => !known.has(line.ProjectID))
      .map(line => ({
        ProjectID: line.ProjectID,
        ProjectName: line.ProjectName,
        Color: line.Color,
        Cells: data.Days.map(day => ({ ...day, Min: 0, Total: "" })),
        TotalMin: 0,
        Total: "",
      }))
    return [...(person.Lines ?? []), ...extra]
  }

  // A line added on this page is not saved until it has hours: it stays only
  // until the page is reloaded.
  function isPendingLine(person: SchedulePerson, line: ScheduleLine): boolean {
    return !person.Lines.some(saved => saved.ProjectID === line.ProjectID)
  }

  function addPending(person: SchedulePerson, projectId: number) {
    const project = data.Projects.find(item => item.ID === projectId)
    if (!project) return
    setPending(previous => ({
      ...previous,
      [person.UserID]: [...(previous[person.UserID] ?? []), { ProjectID: project.ID, ProjectName: project.Name, Color: project.Color }],
    }))
  }

  function cellInput(person: SchedulePerson, line: ScheduleLine, index: number) {
    const key = cellKey(person.UserID, line.ProjectID, index)
    const day = data.Days[index]
    const saved = line.Cells[index]?.Min ?? 0
    const shown = drafts[key] ?? inputValue(saved, format)
    const label = `${person.UserName}, ${line.ProjectName}, ${day.ISO}`
    if (!canEdit) {
      return <span className="font-mono text-sm tabular-nums" aria-label={label}>{saved ? durationLabel(saved, format, lang) : "—"}</span>
    }
    return <Input
      type="text"
      inputMode="decimal"
      autoComplete="off"
      className="h-10 min-w-16 px-1 text-center font-mono text-sm tabular-nums"
      name="hours"
      value={shown}
      placeholder="—"
      title={t(lang, "sch.hourHint")}
      aria-label={label}
      onChange={event => setDrafts(previous => ({ ...previous, [key]: event.target.value }))}
      onBlur={event => void saveCell(person, line, index, event.target.value)}
      onKeyDown={event => {
        if (event.key !== "Enter") return
        // Enter saves the cell and moves to the next hour field in the grid.
        const fields = Array.from(document.querySelectorAll<HTMLInputElement>('main table input[name="hours"]'))
        const field = event.currentTarget
        const next = fields[fields.indexOf(field) + 1]
        field.blur()
        next?.focus()
      }}
    />
  }

  function addProjectSelect(person: SchedulePerson, lines: ScheduleLine[]) {
    const used = new Set(lines.map(line => line.ProjectID))
    const free = data.Projects.filter(project => !used.has(project.ID))
    if (!canEdit || free.length === 0) return null
    return <Select value="" onValueChange={value => addPending(person, Number(value))}>
      <SelectTrigger aria-label={`${t(lang, "sch.addProject")}: ${person.UserName}`} className="h-9 w-full max-w-64 bg-background text-foreground">
        <Plus aria-hidden="true" className="size-4" /><SelectValue placeholder={t(lang, "sch.addProject")} />
      </SelectTrigger>
      <SelectContent>{free.map(project => <SelectItem key={project.ID} value={String(project.ID)}>{project.Name}</SelectItem>)}</SelectContent>
    </Select>
  }

  function adminField(person: SchedulePerson) {
    if (!canEdit) return null
    const shown = adminDrafts[person.UserID] ?? inputValue(person.AdminMin, format)
    return <label className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
      <span className="whitespace-nowrap">{t(lang, "sch.adminHours")}</span>
      <Input
        type="text"
        inputMode="decimal"
        autoComplete="off"
        className="h-8 w-20 px-2 text-center font-mono text-xs tabular-nums"
        name="admin"
        value={shown}
        placeholder="—"
        title={t(lang, "sch.hourHint")}
        aria-label={`${t(lang, "sch.adminHours")}: ${person.UserName}`}
        disabled={saving !== null}
        onChange={event => setAdminDrafts(previous => ({ ...previous, [person.UserID]: event.target.value }))}
        onBlur={event => void saveAdmin(person, event.target.value)}
      />
    </label>
  }

  const copyReady = canEdit && data.CopyFrom && data.WeekEmpty
  const copyHint = !canEdit ? "" : !data.CopyFrom ? t(lang, "sch.copyNoPrev") : !data.WeekEmpty ? t(lang, "sch.copyNeedEmpty") : ""

  return <main className="mx-auto w-full max-w-6xl space-y-4">
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "sch.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, data.CanManage ? "sch.blurb" : "sch.blurbMember")}</p></div>
      <div className="flex flex-col items-end gap-2">
        <nav className="week-nav flex items-center gap-1" aria-label={t(lang, "sch.title")}>
          <Button asChild variant="ghost" size="icon" className="text-foreground" title={t(lang, "sch.prev")}><a href={`/schedule?date=${data.PrevWeek}`} aria-label={t(lang, "sch.prev")}><ArrowLeft aria-hidden="true" /></a></Button>
          <span className="whitespace-nowrap px-2 font-mono text-sm">{data.WeekLabel}</span>
          <Button asChild variant="ghost" size="icon" className="text-foreground" title={t(lang, "sch.next")}><a href={`/schedule?date=${data.NextWeek}`} aria-label={t(lang, "sch.next")}><ArrowRight aria-hidden="true" /></a></Button>
          <Button asChild variant="ghost" size="sm" className="text-foreground"><a href="/schedule">{t(lang, "ts.thisWeek")}</a></Button>
        </nav>
        {canEdit && data.Projects.length > 0 && <div className="flex flex-col items-end gap-1">
          <Button type="button" variant="outline" size="sm" disabled={!copyReady || saving !== null} onClick={() => void copyWeek()}>
            <Copy aria-hidden="true" />{t(lang, "sch.copyWeek")}
          </Button>
          {copyHint && <span className="text-xs text-muted-foreground">{copyHint}</span>}
        </div>}
      </div>
    </header>

    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    {data.Projects.length ? <>
      {data.CanManage && data.People.length === 1 && <p className="text-sm text-muted-foreground">{t(lang, "sch.onlyOwner")} <a href="/settings/invites" className="font-medium text-foreground underline underline-offset-4">{t(lang, "sch.inviteTeam")}</a></p>}

      {!data.CanManage && <p data-readonly-note className="flex items-center gap-2 text-sm text-muted-foreground"><Eye aria-hidden="true" className="size-4 shrink-0" />{t(lang, "sch.readOnly")}</p>}

      {/* A week is seven days wide. Measured: the grid needs 768px of content
          and does not get it at 640 (608px), 768 (736px) or 1024 (992px) —
          the last column falls off the edge. So below the same 1100px
          threshold the timesheet already uses, the sheet is one block per day:
          same inputs, same save path, nothing entered twice, and no sideways
          scroll. 900–1099px get the list; that is the price of the rule. */}
      <div data-sched-days className="grid gap-3 min-[68.75rem]:hidden">
        {data.Days.map((day, dayIndex) => {
          const dayMin = data.People.reduce((sum, person) => sum + linesOf(person).reduce((inner, line) => inner + cellMinutes(person, line, dayIndex), 0), 0)
          return <Card key={day.ISO} className={day.IsToday ? "border-foreground/20" : undefined}>
            <CardHeader className="flex-row items-center justify-between gap-2 px-3 py-2">
              <CardTitle className="text-sm">{day.Label} {day.Date}</CardTitle>
              <span className="font-mono text-sm tabular-nums text-muted-foreground">{t(lang, "sch.dayAll")} {durationLabel(dayMin, format, lang)}</span>
            </CardHeader>
            <CardContent className="grid gap-3 px-3 pb-3">
              {data.People.map(person => {
                const lines = linesOf(person)
                return <div key={person.UserID} className="grid gap-2">
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="min-w-0 truncate text-sm font-medium" title={person.UserName}>{person.UserName}</span>
                    <span className="font-mono text-xs text-muted-foreground">{durationLabel(lines.reduce((sum, line) => sum + cellMinutes(person, line, dayIndex), 0), format, lang)}</span>
                  </div>
                  {lines.map(line => <div key={line.ProjectID} className="flex items-center gap-2">
                    <span className="flex min-w-0 flex-1 items-center gap-2 text-sm"><span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: line.Color }} /><span className="truncate">{line.ProjectName}</span></span>
                    <span className="w-24 shrink-0 text-right">{cellInput(person, line, dayIndex)}</span>
                  </div>)}
                </div>
              })}
            </CardContent>
          </Card>
        })}
      </div>

      {canEdit && <div className="grid gap-2 min-[68.75rem]:hidden">
        {data.People.map(person => {
          const adder = addProjectSelect(person, linesOf(person))
          return adder ? <div key={person.UserID} className="flex items-center justify-between gap-2">
            <span className="min-w-0 truncate text-sm font-medium" title={person.UserName}>{person.UserName}</span>
            {adder}
          </div> : null
        })}
      </div>}

      <Card className="hidden overflow-hidden min-[68.75rem]:block">
        <CardContent className="overflow-x-auto p-0">
          <Table className="week-grid w-full min-w-[48rem] border-collapse text-sm" aria-busy={saving !== null}>
            <colgroup><col className="week-name-col" /><col span={7} /><col className="week-total-col" /></colgroup>
            <TableHeader><TableRow className="border-b">{[
              <TableHead key="member" scope="col" className="sticky left-0 z-10 bg-card px-3 py-3 text-left font-medium">{t(lang, "nav.account")}</TableHead>,
              ...data.Days.map(day => <TableHead key={day.ISO} scope="col" className={`px-1 py-2 text-center font-medium ${day.IsToday ? "bg-muted" : ""}`}><span className="block text-xs uppercase tracking-wider text-muted-foreground">{day.Label}</span><span className="font-mono text-base tabular-nums">{day.Date}</span></TableHead>),
              <TableHead key="total" scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "stats.total")}</TableHead>,
            ]}</TableRow></TableHeader>
            <TableBody>
              {data.People.map(person => {
                const lines = linesOf(person)
                const live = liveTotals(person, lines)
                const adder = addProjectSelect(person, lines)
                return <React.Fragment key={person.UserID}>
                  <TableRow className="border-b bg-muted/30">
                    <TableHead scope="rowgroup" className="sticky left-0 z-10 max-w-56 bg-muted/30 px-3 py-2 text-left font-medium">
                      <span className="grid-name block truncate" title={person.UserName}>{person.UserName}</span>
                      <span className={`block text-xs font-mono ${live.over ? "text-destructive" : "text-muted-foreground"}`}>
                        {t(lang, "sch.planned")} {durationLabel(live.planned, format, lang)} {t(lang, "sch.of")} {person.AvailableLabel}{person.AvailableMin > 0 ? `, ${live.pct}%` : ""}{live.over ? `, ${t(lang, "sch.overload")}` : ""}
                      </span>
                      {data.CanManage && <span className="block text-xs font-mono text-muted-foreground">{t(lang, "sch.tracked")} {person.TrackedLabel}{person.AvailableMin > 0 ? `, ${person.TrackedPct}%` : ""}</span>}
                      {adminField(person)}
                    </TableHead>
                    {live.dayMins.map((minutes, index) => <TableCell key={index} className={`p-1 text-center font-mono text-xs tabular-nums ${data.Days[index].IsToday ? "bg-muted/70" : ""}`}>{minutes ? durationLabel(minutes, format, lang) : "—"}</TableCell>)}
                    <TableCell className="px-3 py-2 text-right font-mono tabular-nums">{durationLabel(live.planned, format, lang)}</TableCell>
                  </TableRow>
                  {lines.map(line => {
                    const total = line.Cells.reduce((sum, _, index) => sum + cellMinutes(person, line, index), 0)
                    return <TableRow key={line.ProjectID} className="border-b last:border-0">
                      <TableHead scope="row" className="sticky left-0 z-10 max-w-56 bg-card px-3 py-2 text-left font-normal">
                        <span className="flex min-w-0 items-center gap-2"><span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: line.Color }} /><span className="truncate">{line.ProjectName}</span></span>
                        {isPendingLine(person, line) && <span className="block text-xs text-muted-foreground">{t(lang, "sch.pendingLine")}</span>}
                      </TableHead>
                      {data.Days.map((day, index) => <TableCell key={day.ISO} className={`p-1 text-center ${day.IsToday ? "bg-muted/70" : ""}`}>{cellInput(person, line, index)}</TableCell>)}
                      <TableCell className="px-3 py-2 text-right font-mono tabular-nums">{total ? durationLabel(total, format, lang) : "—"}</TableCell>
                    </TableRow>
                  })}
                  {adder && <TableRow className="border-b last:border-0">
                    <TableCell colSpan={data.Days.length + 2} className="px-3 py-2">{adder}</TableCell>
                  </TableRow>}
                </React.Fragment>
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <p className="text-sm text-muted-foreground">{t(lang, data.CanManage ? "sch.hint" : "sch.hintMember")}</p>
    </> : <Card><CardContent className="py-7 text-center">
      <p className="font-medium">{t(lang, "sch.empty")}</p>
      <p className="mx-auto mt-1 text-sm text-muted-foreground">{data.CanManage ? t(lang, "sch.emptyHint") : t(lang, "sch.emptyMember")}</p>
      {data.CanManage && <Button asChild className="mt-3" size="sm"><a href="/projects/new"><Plus aria-hidden="true" />{t(lang, "sch.emptyCta")}</a></Button>}
    </CardContent></Card>}
  </main>
}
