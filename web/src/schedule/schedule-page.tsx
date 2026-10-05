import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import * as React from "react"
import { ArrowLeft, ArrowRight, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { ScheduleData, ScheduleRow } from "@/dashboard/types"

type ScheduleUpdate = {
  rows: ScheduleRow[]
  grandTotal: string
  grandMin: number
  error?: string
}

export function SchedulePage({ initial }: { initial: ScheduleData }) {
  const [data, setData] = React.useState(initial)
  const [drafts, setDrafts] = React.useState<Record<string, string>>({})
  const [saving, setSaving] = React.useState<string | null>(null)
  const [error, setError] = React.useState("")
  const lang = data.Lang

  function cellKey(userId: number, index: number) { return `${userId}:${index}` }

  async function saveCell(row: ScheduleRow, cell: ScheduleRow["Cells"][number], value: string) {
    const key = cellKey(row.UserID, cell.Index)
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

    setSaving(key)
    setError("")
    const body = new URLSearchParams({
      csrf_token: data.CSRFToken,
      user_id: String(row.UserID),
      project_id: String(data.ProjectID),
      date: cell.ISO,
      minutes: String(minutes),
    })
    try {
      const response = await fetch("/api/schedule/cell", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
          "X-CSRF-Token": data.CSRFToken,
        },
        body,
      })
      const update = await response.json() as ScheduleUpdate
      if (!response.ok) throw new Error(update.error || response.statusText)
      setData(previous => ({ ...previous, Rows: update.rows, GrandTotal: update.grandTotal, GrandMin: update.grandMin }))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      setDrafts(previous => { const next = { ...previous }; delete next[key]; return next })
    } finally {
      setSaving(null)
    }
  }

  return <main className="mx-auto w-full max-w-6xl space-y-4">
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "sch.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "sch.blurb")}</p></div>
      <nav className="flex items-center gap-1" aria-label={t(lang, "sch.title")}>
        <Button asChild variant="ghost" size="icon" className="text-foreground" title={t(lang, "sch.prev")}><a href={`/schedule?date=${data.PrevWeek}&project=${data.ProjectID}`} aria-label={t(lang, "sch.prev")}><ArrowLeft aria-hidden="true" /></a></Button>
        <span className="whitespace-nowrap px-2 font-mono text-sm">{data.WeekLabel}</span>
        <Button asChild variant="ghost" size="icon" className="text-foreground" title={t(lang, "sch.next")}><a href={`/schedule?date=${data.NextWeek}&project=${data.ProjectID}`} aria-label={t(lang, "sch.next")}><ArrowRight aria-hidden="true" /></a></Button>
        <Button asChild variant="ghost" size="sm" className="text-foreground"><a href={`/schedule?project=${data.ProjectID}`}>{t(lang, "ts.thisWeek")}</a></Button>
      </nav>
    </header>

    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    {data.Projects.length ? <>
      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor="sch-project" className="text-sm text-muted-foreground">{t(lang, "sch.planning")}</Label>
        <Select value={String(data.ProjectID)} onValueChange={project => window.location.assign(`/schedule?date=${data.ThisWeek}&project=${project}`)}>
          <SelectTrigger id="sch-project" className="min-h-10 w-full bg-background text-foreground sm:w-64"><SelectValue /></SelectTrigger>
          <SelectContent>{data.Projects.map(project => <SelectItem key={project.ID} value={String(project.ID)}>{project.Name}</SelectItem>)}</SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground">{t(lang, "sch.planningHint")}</span>
      </div>

      {data.CanManage && data.Rows.length === 1 && <p className="text-sm text-muted-foreground">{t(lang, "sch.onlyOwner")} <a href="/settings/invites" className="font-medium text-foreground underline underline-offset-4">{t(lang, "sch.inviteTeam")}</a></p>}

      <Card className="overflow-hidden">
        <CardContent className="overflow-x-auto p-0">
          <Table className="week-grid w-full min-w-[48rem] border-collapse text-sm" aria-busy={saving !== null}>
            <colgroup><col className="week-name-col" /><col span={7} /><col className="week-total-col" /></colgroup>
            <TableHeader><TableRow className="border-b">{[
              <TableHead key="member" scope="col" className="sticky left-0 z-10 bg-card px-3 py-3 text-left font-medium">{t(lang, "nav.account")}</TableHead>,
              ...data.Days.map(day => <TableHead key={day.ISO} scope="col" className={`px-1 py-2 text-center font-medium ${day.IsToday ? "bg-muted" : ""}`}><span className="block text-xs uppercase tracking-wider text-muted-foreground">{day.Label}</span><span className="font-mono text-base tabular-nums">{day.Date}</span></TableHead>),
              <TableHead key="total" scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "stats.total")}</TableHead>,
            ]}</TableRow></TableHeader>
            <TableBody>
              {data.Rows.map(row => {
                const cells = [
                  <TableHead key="member" scope="row" className="sticky left-0 z-10 max-w-56 bg-card px-3 py-2 text-left font-medium"><span className="grid-name block truncate" title={row.UserName}>{row.UserName}</span><span className="block text-xs font-mono text-muted-foreground">{row.Total}, {row.LoadPct}%</span></TableHead>,
                  ...row.Cells.map(cell => {
                    const key = cellKey(row.UserID, cell.Index)
                    return <TableCell key={cell.Index} className={`p-1 text-center ${cell.IsToday ? "bg-muted/70" : ""}`}>
                      {data.CanManage ? <Input type="number" min={0} max={1440} step={30} inputMode="numeric" className="h-10 min-w-11 px-1 text-center font-mono text-xs tabular-nums" name="minutes" value={drafts[key] ?? (cell.Min ? String(cell.Min) : "")} placeholder="—" title={`${cell.ISO}: ${t(lang, "sch.dayAll")} ${cell.Total}`} aria-label={`${row.UserName} ${cell.ISO}`} disabled={saving !== null} onChange={event => setDrafts(previous => ({ ...previous, [key]: event.target.value }))} onBlur={event => void saveCell(row, cell, event.target.value)} /> : <span className="font-mono tabular-nums" title={`${t(lang, "sch.dayAll")} ${cell.Total}`}>{cell.Min || "—"}</span>}
                    </TableCell>
                  }),
                  <TableCell key="total" className="px-3 py-2 text-right font-mono tabular-nums">{row.Total}</TableCell>,
                ]
                return <TableRow key={row.UserID} className="border-b last:border-0">{cells}</TableRow>
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <p className="text-sm text-muted-foreground">{t(lang, "sch.hint")}</p>
    </> : <Card><CardContent className="py-7 text-center">
      <p className="font-medium">{t(lang, "sch.empty")}</p>
      <p className="mx-auto mt-1 max-w-prose text-sm text-muted-foreground">{data.CanManage ? t(lang, "sch.emptyHint") : t(lang, "sch.emptyMember")}</p>
      {data.CanManage && <Button asChild size="sm" className="mt-4"><a href="/projects/new"><Plus aria-hidden="true" />{t(lang, "sch.emptyCta")}</a></Button>}
    </CardContent></Card>}
  </main>
}
